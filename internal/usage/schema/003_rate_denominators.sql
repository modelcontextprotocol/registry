-- What the stored rates are rates of, so that they can be combined across days
-- and across rows without weighting them by the wrong traffic.
--
-- (There is no file 002. The dashboards' view and functions used to be one;
-- they are now in dashboard_queries.sql, which is applied whenever it changes.)

-- On the scale of requests and gib. Listing requests are the base of
-- cache_hit_pct and of the shares in the listing_use and page_size dimensions.
-- The two compression rates are of listing responses whose encoding is known.
ALTER TABLE usage_daily ADD COLUMN IF NOT EXISTS listing_requests numeric;
ALTER TABLE usage_daily ADD COLUMN IF NOT EXISTS listing_gib numeric;
ALTER TABLE usage_daily ADD COLUMN IF NOT EXISTS compression_known_requests numeric;
ALTER TABLE usage_daily ADD COLUMN IF NOT EXISTS compression_known_gib numeric;

-- Days stored before these columns existed get their listing totals from the
-- endpoint shares stored with them: listing traffic is exactly the two "List
-- servers" endpoints. The compression bases cannot be recovered, so the
-- compression rates of periods that include such days stay empty.
UPDATE usage_daily d
SET listing_requests = d.requests * l.requests_pct / 100, listing_gib = d.gib * l.data_pct / 100
FROM (SELECT day, coalesce(sum(requests_pct) FILTER (WHERE name LIKE 'List servers (%'), 0) AS requests_pct,
             coalesce(sum(data_pct) FILTER (WHERE name LIKE 'List servers (%'), 0) AS data_pct
      FROM usage_share WHERE dimension = 'endpoint' GROUP BY day) l
WHERE l.day = d.day AND d.listing_requests IS NULL;

-- The part of the day's requests that a row's compressed_pct is a rate of:
-- that row's listing responses whose encoding is known.
ALTER TABLE usage_share ADD COLUMN IF NOT EXISTS compression_known_pct numeric;
