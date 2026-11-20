# usage-collector

Records how the hosted registry was used on each full UTC day, and what was published to it.

For a day, it reads the gateway's request counters (for totals) and the gateway's access records in Cloud Logging (for who used the registry and how), and writes that day's totals and breakdowns to Postgres. Grafana reads from there. It can also write the day's report as Markdown.

This program is specific to the hosted instance. It is a separate binary and is not part of the registry server, so nothing here affects self-hosted registries.

## Run it

```bash
make usage-collector            # builds bin/usage-collector

# The scheduled form: refresh the catalogue, then collect every day in the
# look-back window that is not stored yet.
./bin/usage-collector

# Collect chosen days again, replacing what is stored for them.
./bin/usage-collector --day 2026-11-19
./bin/usage-collector --day 2026-11-18,2026-11-19 --markdown-dir reports

# Only the day's report, without connecting to any database.
./bin/usage-collector --day 2026-11-19 --no-store --markdown-dir reports
```

The exit code is non-zero if any day could not be collected and stored. One day failing does not stop the others.

| Flag | Meaning |
| --- | --- |
| `--day` | UTC day to collect, such as `2026-11-19`. Repeat it or separate days with commas. |
| `--markdown-dir` | Also write each day's report to `<dir>/<day>.md`. |
| `--no-store` | Collect and report only. Needs `--day`. Prints the report if no directory is given. |
| `--allow-degraded` | Collect and store a day even though a source it needs was unavailable (see below). |
| `--resync-publishing` | Read every published version again, not only the changed ones. |

## Settings

All settings are environment variables with the prefix `USAGE_COLLECTOR_`. Nothing about a particular environment is built into the program.

| Variable | Meaning | Default |
| --- | --- | --- |
| `GCP_PROJECT` | Google Cloud project that holds the gateway's access records. | required |
| `CLUSTER_NAME` | Cluster that writes them. | required |
| `LOG_NAMESPACE`, `LOG_CONTAINER` | Namespace and container that write them. | `ingress-nginx`, `nginx` |
| `GCP_QUOTA_PROJECT` | Project charged for the log reads. Only needed with a user login. | none |
| `METRICS_URL` | Base URL of the Prometheus-compatible store with the gateway's request counters. | required |
| `DATABASE_URL` | Postgres database for the usage tables. | required unless `--no-store` |
| `REGISTRY_DATABASE_URL` | The registry's database, read for what has been published. Without it the catalogue is left as it is. | none |
| `READER_ROLE` | An existing database role to give read-only access to what the dashboards query. See below. | none |
| `ORGANIZATION_ALIASES` | Names for organizations' own networks, as `KEY=Name;KEY=Name`. See below. | none |
| `LOOKBACK_DAYS` | How far back a run looks for days it has not stored. | `13` |
| `MAX_RECORDS` | About how many access records to read for a day. A day that holds more is read as a sample of about this many. See [How much is read](#how-much-is-read). | `100000` |
| `LOG_READS_PER_MINUTE` | How many requests to Cloud Logging may start in a minute. Cloud Logging allows a project 60. | `50` |
| `DOLLARS_PER_GIB` | Price used for the cost estimate. | `0.10` |
| `CACHE_DIR` | Where the public network reference files are kept between runs. They are downloaded again weekly. | the user cache directory |
| `NETWORK_OWNERS_URL`, `NETWORK_TYPES_URL` | Where those files are downloaded from. | iptoasn.com, peeringdb.com |

Google credentials are found the standard way (Application Default Credentials): a service account in the cluster, or `gcloud auth application-default login` on a workstation. The identity needs permission to read log entries in the project.

### What it needs from the databases

- **Usage database:** it creates and owns the tables, view and functions listed below, so it needs to create objects in the schema the connection uses, and to read and write its own tables. It applies its schema itself at start. Every change only adds; nothing existing is dropped or rewritten.
- **Registry database:** `SELECT` on `servers`, nothing else. It reads in pages of 500 rows, each in its own short read-only transaction with a statement timeout, and pauses between pages. A normal run reads only the versions changed since the previous run. The first run reads every version once.

It takes a session-level advisory lock on the usage database so that two runs do not overlap. Connect directly rather than through a transaction-pooling proxy.

### The dashboards' role

The dashboards should connect with a role that cannot write. Name that role in `READER_ROLE` and the collector grants it, each time it starts and after applying its schema:

- `SELECT` on `usage_daily`, `usage_share`, `publish_version` and the view `usage_detail`;
- `EXECUTE` on `usage_buckets()`, `usage_series()` and `usage_leaderboard()`;
- `USAGE` on the schema they are in, if the role does not have it already.

It grants nothing on its own bookkeeping tables. It does not create the role, and it never revokes anything: removing the setting leaves the grants in place. If the grant fails, the days are still collected and the run exits non-zero.

### Naming an organization's networks

A network is named from its registered description. Hosting providers are recognised by the program. Organizations are not: none is built in.

When an organization's networks are registered under varying descriptions, or its network name should match the product name in its user agent, give it an alias:

```
USAGE_COLLECTOR_ORGANIZATION_ALIASES='EXAMPLECORP=Example Corp;EXAMPLE-CORP=Example Corp'
```

Each pair is a key and a display name. A network whose description contains the key, without regard to case, is shown under that name and counted as the organization's own network. Pairs are separated by semicolons or line breaks, several keys may share a name, and a key needs at least three characters. Aliases are checked before the built-in hosting providers, in the order given.

The value names real organizations, so keep it with the deployment's private configuration rather than in a public repository.

### Running it from a workstation

The counters can be reached through the Kubernetes API server, limited to the one read-only path:

```bash
kubectl --context <context> proxy --port=18428 \
  --accept-paths='^/api/v1/namespaces/monitoring/pods/<metrics-pod>:8428/proxy/api/v1/query$' \
  --reject-methods='^(POST|PUT|PATCH|DELETE)$'

USAGE_COLLECTOR_GCP_PROJECT=<project> USAGE_COLLECTOR_CLUSTER_NAME=<cluster> \
USAGE_COLLECTOR_GCP_QUOTA_PROJECT=<project> \
USAGE_COLLECTOR_METRICS_URL=http://127.0.0.1:18428/api/v1/namespaces/monitoring/pods/<metrics-pod>:8428/proxy \
./bin/usage-collector --day 2026-11-19 --no-store --markdown-dir reports
```

Reading one day's access records takes two to three minutes at the default settings, however busy the day was.

## What it stores

| Object | Contents |
| --- | --- |
| `usage_daily` | One row per UTC day: requests, data sent, and the day's headline rates. |
| `usage_share` | That day's breakdowns, one row per name: organizations, clients, networks, what the unnamed traffic is made of, endpoints, listing use, page sizes, responses, and which servers, namespaces and server types were looked up. |
| `publish_version` | One row per published server version, copied from the registry database. |
| `usage_detail`, `usage_buckets()`, `usage_series()`, `usage_leaderboard()` | The view and functions the dashboards query. They are replaced whenever their definitions change. |
| `usage_collector_state`, `usage_schema_migrations` | What the collector remembers between runs. |

Collecting a day again replaces that day's rows in one transaction.

### What a rate is a rate of

Shares and rates are stored per day. To combine them across days or rows, the view and functions weight each by its own base, which is stored too:

| Figure | Its base |
| --- | --- |
| Shares in most dimensions | all of the day's requests and data: `requests`, `gib` |
| Shares in `listing_use` and `page_size` | the day's listing traffic: `listing_requests`, `listing_gib` |
| `cache_hit_pct` | `listing_requests` |
| `listings_compressed_pct`, `listings_compressed_data_pct` | listing responses whose encoding is known: `compression_known_requests`, `compression_known_gib` |
| A row's `compressed_pct` | that row's such responses, as a share of the day's requests: `compression_known_pct` |
| A row's `not_found_pct`, `top_client_pct` | that row's own lookups |

A figure that cannot be worked out from what is stored is empty rather than approximated. The main case is the client that looked a server up most. Each stored row knows only its own day's winner. Over a range, `usage_leaderboard()` names a winner when every row behind a line has the same one, in which case its share is exact; when the rows disagree, the name and the share are empty.

### How much is read

How many access records Cloud Logging holds for a day is not known in advance. A storage rule there keeps about 1 in 10 fast successful listing records and every other record, but it applies only to fast responses and can be changed or switched off, so on a slow or unusual day almost every record is kept. The collector therefore counts before it reads:

1. It reads a small sample of the day and keeps none of it: 100 records for every `MAX_RECORDS` the day holds. For a day of 5 million records at the default limit that is 5 requests.
2. From that count it picks the fraction that returns about `MAX_RECORDS` records, and reads the day at that fraction. A day that holds fewer is read whole.
3. That read is kept if it fits and is about as large as intended. Otherwise another fraction is tried, at most four more times:
   - A read that returns more than twice `MAX_RECORDS` is stopped. The next fraction is halfway between the largest that is known to fit and the smallest that is known not to.
   - A read that returns less than half of `MAX_RECORDS`, or no registry request at all, is not settled for. The next fraction is larger, chosen from what that read returned.

Step 3 matters only when samples are uneven. Samples are taken on the request ID, and the gateway logs the ID a client sends in `X-Request-ID` when it sends one, so requests that share an ID are all in a sample or all out of it. If no fraction near the limit works, the largest sample that is known to fit is used. The day's `caveats` say that it could not be sampled evenly; they say the same when a sample is kept after another returned too many. If the largest sample that fits holds no registry request, the day fails with "could not be sampled": the records exist, but cannot be collected at this limit.

What a sample returned is only ever used to choose the next fraction. Nothing is concluded from a sample about the rest of the day: not that the day is as busy as the part a stopped read saw, and not that records are absent because a sample held none.

So a normal day costs about `MAX_RECORDS / 1000` requests for the read, plus one for every million records it holds. No attempt keeps more than twice `MAX_RECORDS` records in memory. A day with very uneven samples can cost up to six such reads, on every run until it leaves the look-back window if it fails. Requests are spaced to stay under `LOG_READS_PER_MINUTE`, across days as well.

Reading the same day again at the same fraction returns the same records.

### How well a day was measured

The collector scales the listing records back up to the gateway's counters, hour by hour, whatever share of them was kept in that hour. In an hour that kept no fast listing record at all, which is common when there is little traffic, the requests the counters saw are carried by the records of the nearest hour that kept some. Each day's row records how the scaling went:

| Column | Meaning |
| --- | --- |
| `log_requests_ratio`, `log_data_ratio` | The log-based estimate divided by the counters. Close to 1 when the access records and the counters describe the same traffic. A lasting drift away from 1 means the log format, the storage rule or the counters have changed. On a day with few listing records the data ratio is noisy, because a handful of records stand for all the listing data. |
| `sample_fraction` | The fraction of records Cloud Logging was asked for. |
| `listing_weight_min`, `listing_weight_max` | The range of hourly scaling factors for sampled listing records. |
| `totals_source` | `counters`; `logs` if the counters were unavailable; `mixed` if only one of requests and data sent came from the counters. |
| `caveats` | Anything that makes the day's shares less trustworthy. |
| `records_sampled`, `background_records` | Access records used, and records ignored as background. |

### Missing and degraded days

A run without `--day` collects every full UTC day in the look-back window that has no row, oldest first. A missed night, or the first run, needs nothing else. A day can be collected once it has been over for 15 minutes, whether it is named or not.

The gateway counters are kept for 14 days. A day older than that cannot be scaled, which is why the window defaults to 13 days.

A day is normally **not collected** if something it needs could not be used: the gateway counters, the network reference files, or (when the registry database is configured) an up-to-date catalogue. This is known before the day's access records are read, so they are not read. The day stays missing and the next run collects it. Storing such a day would be permanent, because a stored day is not collected again on its own.

**Last chance.** The oldest day in the window is the exception, because the next scheduled run no longer looks at it. It is stored with whatever is missing recorded in `caveats`, rather than lost:

| Missing on the last chance | Result |
| --- | --- |
| Network-owner table | Stored. Networks show as Unknown. |
| Network-type list | Stored. Fewer networks are recognised as internet providers. |
| Up-to-date catalogue | Stored. Lookups of recently published servers may count as unknown names. |
| Hourly counters | Stored. One scaling factor is used for the whole day. |
| Data-volume counter only | Stored. Data sent is estimated from the access records; `totals_source` is `mixed`. |
| Request counters | **Not stored.** Without them the records cannot be scaled to the gateway's totals. |

To collect a degraded day at any other time, name it with `--day` and add `--allow-degraded`. That also applies without the request counters; the read is sized the same way.

**Days with nothing to collect.** A day is skipped, without failing the run and without storing anything, when all of this holds: the gateway's request counter has series that day, they show no registry request, and none of the day's access records is a registry request. The last is checked by reading every record of the day, not a sample; a day with more than twice `MAX_RECORDS` records is not skipped, because not all of them are looked at. A day for which neither the counters nor Cloud Logging hold anything at all is different: that cannot be told from a collector pointed at the wrong place, so it fails like any day without counters. And when the counters did count requests but the whole day holds no access record of one, the day fails too: the records are somewhere the log settings do not point, or were not written.

## Privacy

- Client addresses are used only in memory, to count distinct sources and to look up the network owner. They are never logged, written to the report, or stored.
- A requested server name is stored only if that server is published. Every other requested name is counted in one group, "Names not in the catalogue", and is never written down.
- User agents are reduced to a product name of at most 28 plain characters. Requested page sizes are reduced to 8 letters or digits.
- A product name that says who or where the client is, rather than what it is, is not kept. A fixed label is stored in its place and the client counts as unnamed:

  | The product name is | Stored as |
  | --- | --- |
  | an IPv4 or IPv6 address, with or without a port, also inside a longer name; a hardware address | `(address withheld)` |
  | someone at somewhere, such as `name@example.org` or `name@host` | `(email withheld)` |
  | a host name that only resolves inside a private network (`.local`, `.internal`, `.lan`, `.corp`, `.home`, `.private`, `.intranet`, `.localdomain`, `.arpa`, `localhost`) | `(host name withheld)` |
  | a UUID, or 16 or more hexadecimal digits in a row | `(identifier withheld)` |

  The rule errs towards withholding: a four-part version number cannot be told from an IPv4 address and is withheld too. It cannot catch everything. A person's name used as a product name, or a host name under a public domain, looks like any other product name and is kept.
- The collector's own log contains counts and status codes. It contains no access records, addresses, request paths or credentials.

`TestNothingSensitiveLeavesTheProcess` checks all of this against the stored rows, the report and the log, including the names taken out of user agents.

Requests to the dashboards' own host and to the gateway's test hosts are not registry traffic. The gateway's counters leave them out, and so does the collector. `TestFixtureHostsMatchTheGateway` fails if the two disagree about which hosts those are.

## Tests

```bash
make test-unit                         # includes ./internal/usage
go test ./internal/usage               # needs PostgreSQL on localhost:5432
```

The tests run whole days from synthetic access records. See [the fixture notes](../../internal/usage/testdata/README.md) for where the expected output comes from.
