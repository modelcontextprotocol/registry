-- What the dashboards query. Keeping the arithmetic here means every panel shares it.

-- Shares turned into estimated volumes, which can be added up across days.
CREATE OR REPLACE VIEW usage_detail AS
SELECT s.day, s.dimension, s.name, s.kind, s.identified_by, s.category, s.rollup_count, s.compressed_pct, s.addresses,
       s.not_found_pct, s.top_client_pct,
       s.requests_pct / 100 * d.requests AS requests,
       s.data_pct / 100 * d.gib AS gib
FROM usage_share s JOIN usage_daily d USING (day);

-- One row per day, week or month in the range. "time" is the first day with data,
-- so a period cut short by the range still plots inside it. Rates are weighted by volume;
-- the name counts are averages per day, because distinct counts cannot be added across days.
CREATE OR REPLACE FUNCTION usage_buckets(period text, t_from timestamptz, t_to timestamptz)
RETURNS TABLE (bucket timestamptz, "time" timestamptz, days bigint, days_in_period integer, requests numeric, gib numeric,
               listings_compressed_pct numeric, listings_compressed_data_pct numeric, cache_hit_pct numeric,
               named_orgs numeric, user_agents numeric)
LANGUAGE sql STABLE AS $$
  SELECT date_trunc(period, d.day, 'UTC'), min(d.day), count(*),
         CASE period WHEN 'day' THEN 1 WHEN 'week' THEN 7
              ELSE extract(day FROM date_trunc('month', min(d.day) AT TIME ZONE 'UTC') + interval '1 month - 1 day')::integer END,
         sum(d.requests), sum(d.gib),
         sum(d.listings_compressed_pct * d.requests) / nullif(sum(d.requests) FILTER (WHERE d.listings_compressed_pct IS NOT NULL), 0),
         sum(d.listings_compressed_data_pct * d.gib) / nullif(sum(d.gib) FILTER (WHERE d.listings_compressed_data_pct IS NOT NULL), 0),
         sum(d.cache_hit_pct * d.requests) / nullif(sum(d.requests) FILTER (WHERE d.cache_hit_pct IS NOT NULL), 0),
         avg(d.named_orgs), avg(d.user_agents)
  FROM usage_daily d
  WHERE d.day >= t_from AND d.day <= t_to
  GROUP BY 1
$$;

-- One row per name per period, with the period's totals alongside for shares and per-day averages.
-- pos ranks named rows by data within the period.
CREATE OR REPLACE FUNCTION usage_series(dim text, period text, t_from timestamptz, t_to timestamptz)
RETURNS TABLE ("time" timestamptz, name text, kind text, pos bigint, requests numeric, gib numeric,
               days bigint, bucket_requests numeric, bucket_gib numeric)
LANGUAGE sql STABLE AS $$
  WITH b AS (SELECT * FROM usage_buckets(period, t_from, t_to)),
  r AS (
    SELECT date_trunc(period, s.day, 'UTC') AS bucket, s.name, s.kind, sum(s.requests) AS requests, sum(s.gib) AS gib
    FROM usage_detail s
    WHERE s.dimension = dim AND s.day >= t_from AND s.day <= t_to
    GROUP BY 1, 2, 3)
  SELECT b."time", r.name, r.kind,
         CASE WHEN r.kind = 'named'
              THEN row_number() OVER (PARTITION BY r.bucket, r.kind ORDER BY r.gib DESC NULLS LAST, r.requests DESC NULLS LAST) END,
         r.requests, r.gib, b.days, b.requests, b.gib
  FROM r JOIN b USING (bucket)
$$;

-- The whole range as one ranked list: the top "keep" names by data, everything smaller folded
-- into an 'other' row, then the unnamed rows. With by_category, ranking and folding happen
-- within each category. With by_requests, names are ranked by requests instead of data.
CREATE OR REPLACE FUNCTION usage_leaderboard(dim text, t_from timestamptz, t_to timestamptz, keep integer, by_category boolean DEFAULT false,
                                             by_requests boolean DEFAULT false)
RETURNS TABLE (pos bigint, name text, kind text, identified_by text, category text, names_per_day numeric,
               requests_pct numeric, data_pct numeric, requests numeric, gib numeric, compressed_pct numeric,
               addresses_per_day numeric, days_seen bigint, days bigint, not_found_pct numeric, top_client_pct numeric)
LANGUAGE sql STABLE AS $$
  WITH total AS (
    SELECT count(*) AS days, sum(d.requests) AS requests, sum(d.gib) AS gib
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
           CASE WHEN bool_or(l.identified_by ILIKE '%user agent%') AND bool_or(l.identified_by ILIKE '%network owner%')
                     THEN 'User agent and network owner'
                WHEN bool_or(l.identified_by ILIKE '%user agent%') THEN 'User agent'
                WHEN bool_or(l.identified_by ILIKE '%network owner%') THEN 'Network owner'
                ELSE mode() WITHIN GROUP (ORDER BY l.identified_by) END AS identified_by,
           sum(l.requests) AS requests, sum(l.gib) AS gib,
           sum(l.compressed_pct * l.requests) / nullif(sum(l.requests) FILTER (WHERE l.compressed_pct IS NOT NULL), 0) AS compressed_pct,
           sum(l.not_found_pct * l.requests) / nullif(sum(l.requests) FILTER (WHERE l.not_found_pct IS NOT NULL), 0) AS not_found_pct,
           sum(l.top_client_pct * l.requests) / nullif(sum(l.requests) FILTER (WHERE l.top_client_pct IS NOT NULL), 0) AS top_client_pct,
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
         100 * w.requests / t.requests, 100 * w.gib / t.gib, w.requests, w.gib, w.compressed_pct,
         w.addresses::numeric / t.days, w.days_seen, t.days, w.not_found_pct, w.top_client_pct
  FROM weighed w CROSS JOIN total t
$$;
