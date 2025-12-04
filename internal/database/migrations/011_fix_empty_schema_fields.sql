-- Migration: Fix empty $schema fields in 17 specific server entries
--
-- This migration updates exactly 17 server entries that were published with
-- empty $schema fields (""). These entries are explicitly listed below to
-- ensure no other data is affected.
--
-- Issue: https://github.com/modelcontextprotocol/registry/issues/805
--
-- Affected entries (17 total):
--   io.github.OtherVibes/mcp-as-a-judge: 0.3.12, 0.3.13, 0.3.14, 0.3.20
--   io.github.Skills03/scrimba-teaching: 1.0.1, 1.1.0, 1.2.0
--   io.github.antuelle78/weather-mcp: 1.0.0
--   io.github.jkakar/cookwith-mcp: 1.0.0, 1.0.1, 1.0.2
--   io.github.ruvnet/claude-flow: 2.0.0-alpha.104, 2.0.0-alpha.105
--   io.github.ruvnet/ruv-swarm: 1.0.18, 1.0.19
--   io.github.toby/mirror-mcp: 0.0.4
--   travel.kismet/mcp-server: 0.0.0

BEGIN;

-- Define the exact list of affected entries
-- Using a CTE to make the affected entries explicit and auditable
WITH affected_entries AS (
    SELECT server_name, version FROM (VALUES
        -- io.github.OtherVibes/mcp-as-a-judge (4 versions)
        ('io.github.OtherVibes/mcp-as-a-judge', '0.3.12'),
        ('io.github.OtherVibes/mcp-as-a-judge', '0.3.13'),
        ('io.github.OtherVibes/mcp-as-a-judge', '0.3.14'),
        ('io.github.OtherVibes/mcp-as-a-judge', '0.3.20'),
        -- io.github.Skills03/scrimba-teaching (3 versions)
        ('io.github.Skills03/scrimba-teaching', '1.0.1'),
        ('io.github.Skills03/scrimba-teaching', '1.1.0'),
        ('io.github.Skills03/scrimba-teaching', '1.2.0'),
        -- io.github.antuelle78/weather-mcp (1 version)
        ('io.github.antuelle78/weather-mcp', '1.0.0'),
        -- io.github.jkakar/cookwith-mcp (3 versions)
        ('io.github.jkakar/cookwith-mcp', '1.0.0'),
        ('io.github.jkakar/cookwith-mcp', '1.0.1'),
        ('io.github.jkakar/cookwith-mcp', '1.0.2'),
        -- io.github.ruvnet/claude-flow (2 versions)
        ('io.github.ruvnet/claude-flow', '2.0.0-alpha.104'),
        ('io.github.ruvnet/claude-flow', '2.0.0-alpha.105'),
        -- io.github.ruvnet/ruv-swarm (2 versions)
        ('io.github.ruvnet/ruv-swarm', '1.0.18'),
        ('io.github.ruvnet/ruv-swarm', '1.0.19'),
        -- io.github.toby/mirror-mcp (1 version)
        ('io.github.toby/mirror-mcp', '0.0.4'),
        -- travel.kismet/mcp-server (1 version)
        ('travel.kismet/mcp-server', '0.0.0')
    ) AS t(server_name, version)
)
UPDATE servers s
SET value = jsonb_set(
    s.value,
    '{$schema}',
    '"https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json"'::jsonb
)
FROM affected_entries ae
WHERE s.server_name = ae.server_name
  AND s.version = ae.version
  AND (s.value->>'$schema' = '' OR s.value->>'$schema' IS NULL);

COMMIT;
