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
| `MAX_RECORDS` | Access records to read for a day before asking Cloud Logging for a sample instead. | `100000` |
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

Reading one day's access records takes a few minutes.

## What it stores

| Object | Contents |
| --- | --- |
| `usage_daily` | One row per UTC day: requests, data sent, and the day's headline rates. |
| `usage_share` | That day's breakdowns, one row per name: organizations, clients, networks, what the unnamed traffic is made of, endpoints, listing use, page sizes, responses, and which servers, namespaces and server types were looked up. |
| `publish_version` | One row per published server version, copied from the registry database. |
| `usage_detail`, `usage_buckets()`, `usage_series()`, `usage_leaderboard()` | The view and functions the dashboards query. |
| `usage_collector_state`, `usage_schema_migrations` | What the collector remembers between runs. |

Collecting a day again replaces that day's rows in one transaction.

### How well a day was measured

Cloud Logging keeps about 1 in 10 fast successful listing records and every other record. The collector scales the listing records back up to the gateway's counters, hour by hour. Each day's row records how that went:

| Column | Meaning |
| --- | --- |
| `log_requests_ratio`, `log_data_ratio` | The log-based estimate divided by the counters. Close to 1 when the access records and the sampling rule are what the collector expects. A drift away from 1 means the log format or the sampling rule has changed. |
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
| Request counters | **Not stored.** Without them the read cannot be sampled and the records cannot be scaled to the gateway's totals. |

To collect a degraded day at any other time, name it with `--day` and add `--allow-degraded`. That also applies without the request counters, but then the read is not sampled and reads every record of the day; a read that passes ten times `MAX_RECORDS` is abandoned rather than allowed to fill memory.

## Privacy

- Client addresses are used only in memory, to count distinct sources and to look up the network owner. They are never logged, written to the report, or stored.
- A requested server name is stored only if that server is published. Every other requested name is counted in one group, "Names not in the catalogue", and is never written down.
- User agents are reduced to a product name of at most 28 plain characters. Requested page sizes are reduced to 8 letters or digits.
- The collector's own log contains counts and status codes. It contains no access records, addresses, request paths or credentials.

`TestNothingSensitiveLeavesTheProcess` checks all of this against the stored rows, the report and the log.

## Tests

```bash
make test-unit                         # includes ./internal/usage
go test ./internal/usage               # needs PostgreSQL on localhost:5432
```

The tests run whole days from synthetic access records. See [the fixture notes](../../internal/usage/testdata/README.md) for where the expected output comes from.
