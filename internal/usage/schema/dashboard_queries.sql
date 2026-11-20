-- What the dashboards query. Keeping the arithmetic here means every panel shares it.
--
-- This file is applied whenever its content changes. It may only replace
-- definitions: a function keeps its arguments and result columns, and the view
-- keeps its columns in order, with new ones added at the end.
--
-- Two rules hold throughout.
-- 1. A rate is combined across days or rows weighted by what it is a rate of.
-- 2. A figure that cannot be worked out from what is stored is NULL, not a guess. That includes
--    a total or a rate for a period in which some day lacks what the figure is made of.
--
-- Shares in the listing_use and page_size dimensions are of the day's listing
-- traffic. Shares in every other dimension are of all of the day's traffic.

-- Shares turned into estimated volumes, which can be added up across days.
-- compression_known_requests is what compressed_pct is a rate of.
CREATE OR REPLACE VIEW usage_detail AS
SELECT s.day, s.dimension, s.name, s.kind, s.identified_by, s.category, s.rollup_count, s.compressed_pct, s.addresses,
       s.not_found_pct, s.top_client_pct,
       s.requests_pct / 100 * CASE WHEN s.dimension IN ('listing_use', 'page_size') THEN d.listing_requests ELSE d.requests END AS requests,
       s.data_pct / 100 * CASE WHEN s.dimension IN ('listing_use', 'page_size') THEN d.listing_gib ELSE d.gib END AS gib,
       s.compression_known_pct / 100 * d.requests AS compression_known_requests
FROM usage_share s JOIN usage_daily d USING (day);

-- One row per day, week or month in the range. "time" is the first day with data,
-- so a period cut short by the range still plots inside it. Each rate is weighted by its own base:
-- the compression rates by the listing responses whose encoding is known, the cache rate by listing
-- requests. The name counts are averages per day, because distinct counts cannot be added across days.
CREATE OR REPLACE FUNCTION usage_buckets(period text, t_from timestamptz, t_to timestamptz)
RETURNS TABLE (bucket timestamptz, "time" timestamptz, days bigint, days_in_period integer, requests numeric, gib numeric,
               listings_compressed_pct numeric, listings_compressed_data_pct numeric, cache_hit_pct numeric,
               named_orgs numeric, user_agents numeric)
LANGUAGE sql STABLE AS $$
  SELECT date_trunc(period, d.day, 'UTC'), min(d.day), count(*),
         CASE period WHEN 'day' THEN 1 WHEN 'week' THEN 7
              ELSE extract(day FROM date_trunc('month', min(d.day) AT TIME ZONE 'UTC') + interval '1 month - 1 day')::integer END,
         sum(d.requests), sum(d.gib),
         CASE WHEN count(*) FILTER (WHERE d.listings_compressed_pct IS NOT NULL AND d.compression_known_requests IS NULL) = 0
              THEN sum(d.listings_compressed_pct * d.compression_known_requests)
                   / nullif(sum(d.compression_known_requests) FILTER (WHERE d.listings_compressed_pct IS NOT NULL), 0) END,
         CASE WHEN count(*) FILTER (WHERE d.listings_compressed_data_pct IS NOT NULL AND d.compression_known_gib IS NULL) = 0
              THEN sum(d.listings_compressed_data_pct * d.compression_known_gib)
                   / nullif(sum(d.compression_known_gib) FILTER (WHERE d.listings_compressed_data_pct IS NOT NULL), 0) END,
         CASE WHEN count(*) FILTER (WHERE d.cache_hit_pct IS NOT NULL AND d.listing_requests IS NULL) = 0
              THEN sum(d.cache_hit_pct * d.listing_requests)
                   / nullif(sum(d.listing_requests) FILTER (WHERE d.cache_hit_pct IS NOT NULL), 0) END,
         avg(d.named_orgs), avg(d.user_agents)
  FROM usage_daily d
  WHERE d.day >= t_from AND d.day <= t_to
  GROUP BY 1
$$;

-- One row per name per period, with the period's totals alongside for shares and per-day averages.
-- The totals are of the traffic the dimension's shares are of, so requests / bucket_requests is
-- always a share of the right whole. pos ranks named rows by data within the period.
CREATE OR REPLACE FUNCTION usage_series(dim text, period text, t_from timestamptz, t_to timestamptz)
RETURNS TABLE ("time" timestamptz, name text, kind text, pos bigint, requests numeric, gib numeric,
               days bigint, bucket_requests numeric, bucket_gib numeric)
LANGUAGE sql STABLE AS $$
  WITH b AS (
    SELECT date_trunc(period, d.day, 'UTC') AS bucket, min(d.day) AS "time", count(*) AS days,
           CASE WHEN dim NOT IN ('listing_use', 'page_size') THEN sum(d.requests)
                WHEN count(d.listing_requests) = count(*) THEN sum(d.listing_requests) END AS requests,
           CASE WHEN dim NOT IN ('listing_use', 'page_size') THEN sum(d.gib)
                WHEN count(d.listing_gib) = count(*) THEN sum(d.listing_gib) END AS gib
    FROM usage_daily d
    WHERE d.day >= t_from AND d.day <= t_to
    GROUP BY 1),
  r AS (
    SELECT date_trunc(period, s.day, 'UTC') AS bucket, s.name, s.kind, sum(s.requests) AS requests, sum(s.gib) AS gib
    FROM usage_detail s
    WHERE s.dimension = dim AND s.day >= t_from AND s.day <= t_to
    GROUP BY 1, 2, 3)
  SELECT b."time", r.name, r.kind,
         CASE WHEN r.kind = 'named'
              THEN row_number() OVER (PARTITION BY r.bucket, r.kind ORDER BY r.gib DESC NULLS LAST, r.requests DESC NULLS LAST) END,
         CASE WHEN b.requests IS NOT NULL THEN r.requests END, CASE WHEN b.gib IS NOT NULL THEN r.gib END,
         b.days, b.requests, b.gib
  FROM r JOIN b USING (bucket)
$$;

-- The whole range as one ranked list: the top "keep" names by data, everything smaller folded
-- into an 'other' row, then the unnamed rows. With by_category, ranking and folding happen
-- within each category. With by_requests, names are ranked by requests instead of data.
--
-- For the lookup dimensions, identified_by is the client that asked most and top_client_pct its
-- share. Only each stored row's own winner is known. When every row behind a line has the same
-- winner, that client is the line's winner too and its share is exact. When the rows disagree,
-- the line's winner cannot be told from what is stored, and both columns are NULL.
CREATE OR REPLACE FUNCTION usage_leaderboard(dim text, t_from timestamptz, t_to timestamptz, keep integer, by_category boolean DEFAULT false,
                                             by_requests boolean DEFAULT false)
RETURNS TABLE (pos bigint, name text, kind text, identified_by text, category text, names_per_day numeric,
               requests_pct numeric, data_pct numeric, requests numeric, gib numeric, compressed_pct numeric,
               addresses_per_day numeric, days_seen bigint, days bigint, not_found_pct numeric, top_client_pct numeric)
LANGUAGE sql STABLE AS $$
  WITH total AS (
    SELECT count(*) AS days,
           CASE WHEN dim NOT IN ('listing_use', 'page_size') THEN sum(d.requests)
                WHEN count(d.listing_requests) = count(*) THEN sum(d.listing_requests) END AS requests,
           CASE WHEN dim NOT IN ('listing_use', 'page_size') THEN sum(d.gib)
                WHEN count(d.listing_gib) = count(*) THEN sum(d.listing_gib) END AS gib
    FROM usage_daily d
    WHERE d.day >= t_from AND d.day <= t_to),
  src AS (
    SELECT s.*, CASE WHEN by_category THEN s.category END AS grp
    FROM usage_detail s
    WHERE s.dimension = dim AND s.day >= t_from AND s.day <= t_to),
  ranked AS (
    SELECT s.name, s.grp,
           row_number() OVER (PARTITION BY s.grp ORDER BY CASE WHEN by_requests THEN sum(s.requests) ELSE sum(s.gib) END DESC NULLS LAST,
                                                         sum(s.requests) DESC NULLS LAST) AS place
    FROM src s
    WHERE s.kind = 'named'
    GROUP BY s.name, s.grp),
  labelled AS (
    SELECT s.*, CASE WHEN s.kind = 'unnamed' THEN 'unnamed'
                     WHEN s.kind = 'named' AND r.place <= keep THEN 'named'
                     ELSE 'other' END AS shown
    FROM src s LEFT JOIN ranked r ON s.kind = 'named' AND r.name = s.name AND r.grp IS NOT DISTINCT FROM s.grp),
  grouped AS (
    SELECT l.shown, CASE WHEN l.shown = 'other' THEN 'Other' ELSE l.name END AS label, l.grp,
           mode() WITHIN GROUP (ORDER BY l.category) FILTER (WHERE l.shown <> 'other') AS seen_category,
           CASE WHEN dim IN ('server', 'server_namespace', 'server_type')
                     THEN CASE WHEN count(DISTINCT l.identified_by) = 1 AND count(l.identified_by) = count(*)
                                    AND count(l.top_client_pct) = count(*) THEN min(l.identified_by) END
                WHEN bool_or(l.identified_by ILIKE '%user agent%') AND bool_or(l.identified_by ILIKE '%network owner%')
                     THEN 'User agent and network owner'
                WHEN bool_or(l.identified_by ILIKE '%user agent%') THEN 'User agent'
                WHEN bool_or(l.identified_by ILIKE '%network owner%') THEN 'Network owner'
                ELSE mode() WITHIN GROUP (ORDER BY l.identified_by) END AS identified_by,
           sum(l.requests) AS requests, sum(l.gib) AS gib,
           CASE WHEN count(*) FILTER (WHERE l.compressed_pct IS NOT NULL AND l.compression_known_requests IS NULL) = 0
                THEN sum(l.compressed_pct * l.compression_known_requests)
                     / nullif(sum(l.compression_known_requests) FILTER (WHERE l.compressed_pct IS NOT NULL), 0) END AS compressed_pct,
           sum(l.not_found_pct * l.requests) / nullif(sum(l.requests) FILTER (WHERE l.not_found_pct IS NOT NULL), 0) AS not_found_pct,
           CASE WHEN count(DISTINCT l.identified_by) = 1 AND count(l.identified_by) = count(*) AND count(l.top_client_pct) = count(*)
                THEN sum(l.top_client_pct * l.requests) / nullif(sum(l.requests), 0) END AS top_client_pct,
           sum(l.addresses) AS addresses,
           coalesce(sum(l.rollup_count), 0) + count(*) FILTER (WHERE l.kind = 'named') AS names,
           count(DISTINCT l.day) AS days_seen
    FROM labelled l
    GROUP BY 1, 2, 3),
  weighed AS (
    SELECT g.*, CASE WHEN by_category THEN sum(g.gib) OVER (PARTITION BY g.grp) END AS grp_gib
    FROM grouped g)
  SELECT row_number() OVER (ORDER BY w.grp_gib DESC NULLS LAST,
                                     CASE w.shown WHEN 'named' THEN 1 WHEN 'other' THEN 2 ELSE 3 END,
                                     CASE WHEN by_requests THEN w.requests ELSE w.gib END DESC NULLS LAST, w.requests DESC NULLS LAST),
         w.label, w.shown, w.identified_by, coalesce(w.grp, w.seen_category),
         CASE WHEN w.shown = 'other' THEN w.names::numeric / t.days END,
         100 * w.requests / nullif(t.requests, 0), 100 * w.gib / nullif(t.gib, 0),
         CASE WHEN t.requests IS NOT NULL THEN w.requests END, CASE WHEN t.gib IS NOT NULL THEN w.gib END, w.compressed_pct,
         w.addresses::numeric / t.days, w.days_seen, t.days, w.not_found_pct, w.top_client_pct
  FROM weighed w CROSS JOIN total t
$$;
