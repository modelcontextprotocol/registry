-- Tables for the registry usage dashboards. One UTC day per row in usage_daily,
-- with that day's breakdowns in usage_share; publish_version mirrors the
-- catalogue. Every statement can be run again without changing anything.

-- One row per published server version. type_label is a single label per
-- version (for stacked charts); offers lists every package type and remote it has.
CREATE TABLE IF NOT EXISTS publish_version (
  name text NOT NULL,
  version text NOT NULL,
  namespace text NOT NULL,
  namespace_kind text NOT NULL,
  published_at timestamptz NOT NULL,
  status text,
  is_latest boolean NOT NULL,
  first_for_server boolean NOT NULL,
  type_label text NOT NULL,
  offers text[] NOT NULL,
  PRIMARY KEY (name, version)
);
CREATE INDEX IF NOT EXISTS publish_version_published_at ON publish_version (published_at);

CREATE TABLE IF NOT EXISTS usage_daily (
  day timestamptz PRIMARY KEY,
  requests bigint,
  gib numeric,
  cost_usd numeric,
  user_agents integer,
  named_orgs integer,
  named_requests_pct numeric,
  named_data_pct numeric,
  listings_compressed_pct numeric,
  listings_compressed_data_pct numeric,
  cache_hit_pct numeric,
  records_sampled integer
);

-- How the day was collected. The two ratios are the log-based estimate divided
-- by the gateway counters: close to 1 when the access records and the sampling
-- rule are what the collector expects, and NULL when the counters were
-- unavailable. totals_source says where requests and gib came from: 'counters',
-- 'logs', or 'mixed' when only one of the two came from the counters.
ALTER TABLE usage_daily ADD COLUMN IF NOT EXISTS log_requests_ratio numeric;
ALTER TABLE usage_daily ADD COLUMN IF NOT EXISTS log_data_ratio numeric;
ALTER TABLE usage_daily ADD COLUMN IF NOT EXISTS sample_fraction numeric;
ALTER TABLE usage_daily ADD COLUMN IF NOT EXISTS listing_weight_min numeric;
ALTER TABLE usage_daily ADD COLUMN IF NOT EXISTS listing_weight_max numeric;
ALTER TABLE usage_daily ADD COLUMN IF NOT EXISTS background_records integer;
ALTER TABLE usage_daily ADD COLUMN IF NOT EXISTS totals_source text;
ALTER TABLE usage_daily ADD COLUMN IF NOT EXISTS caveats text[];
ALTER TABLE usage_daily ADD COLUMN IF NOT EXISTS collected_at timestamptz;
ALTER TABLE usage_daily ADD COLUMN IF NOT EXISTS collector_version text;

-- kind: 'named' for one name, 'other' for the names grouped below the day's list,
-- 'unnamed' for clients that cannot be tied to a name (or, for lookups, names not in the catalogue).
-- For the lookup dimensions (server, server_namespace, server_type) identified_by holds the client
-- that asked most, top_client_pct that client's share of the lookups, category the server's type
-- or how the namespace was proved, and not_found_pct the share of lookups that came back not found.
-- For listing_use and page_size the shares are of listing traffic, not of all traffic.
CREATE TABLE IF NOT EXISTS usage_share (
  day timestamptz NOT NULL REFERENCES usage_daily,
  dimension text NOT NULL,
  name text NOT NULL,
  kind text NOT NULL,
  rank integer NOT NULL,
  requests_pct numeric,
  data_pct numeric,
  compressed_pct numeric,
  addresses integer,
  identified_by text,
  category text,
  rollup_count integer,
  not_found_pct numeric,
  top_client_pct numeric,
  PRIMARY KEY (day, dimension, rank)
);

-- What the collector remembers between runs, such as how far it has read the catalogue.
CREATE TABLE IF NOT EXISTS usage_collector_state (
  key text PRIMARY KEY,
  value text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);
