-- Migration: Set NuGet package registryBaseUrl for specific server entries
--
-- This migration updates the packages[].registryBaseUrl to
-- "https://api.nuget.org/v3/index.json" for NuGet packages on a small,
-- explicitly listed set of server entries. Only packages with
-- registryType == "nuget" are modified; other package types are left
-- unchanged.
--
-- Entries to update (name, version):
--   com.joelverhagen.mcp/Knapcode.SampleMcpServer    0.7.0-beta, 0.10.0-beta.10
--   io.github.joelverhagen/Knapcode.SampleMcpServer  0.7.0-beta
--   io.github.moonolgerd/game-mcp                    1.0.0
--   io.github.timheuer/sampledotnetmcpserver         0.1.56-beta-g9538a23d37, 0.1.57-beta

BEGIN;

WITH affected_entries AS (
    SELECT server_name, version FROM (VALUES
        ('com.joelverhagen.mcp/Knapcode.SampleMcpServer',   '0.7.0-beta'),
        ('com.joelverhagen.mcp/Knapcode.SampleMcpServer',   '0.10.0-beta.10'),
        ('io.github.joelverhagen/Knapcode.SampleMcpServer', '0.7.0-beta'),
        ('io.github.moonolgerd/game-mcp',                   '1.0.0'),
        ('io.github.timheuer/sampledotnetmcpserver',        '0.1.56-beta-g9538a23d37'),
        ('io.github.timheuer/sampledotnetmcpserver',        '0.1.57-beta')
    ) AS t(server_name, version)
)
UPDATE servers s
SET value = jsonb_set(
    s.value,
    '{packages}',
    (
        SELECT jsonb_agg(
            CASE
                WHEN p->>'registryType' = 'nuget' THEN
                    jsonb_set(
                        p,
                        '{registryBaseUrl}',
                        '"https://api.nuget.org/v3/index.json"'::jsonb,
                        true
                    )
                ELSE p
            END
        )
        FROM jsonb_array_elements(s.value#>'{packages}') AS p
    ),
    true
)
FROM affected_entries ae
WHERE s.server_name = ae.server_name
    AND s.version = ae.version
    AND s.value#>'{packages}' IS NOT NULL
    AND EXISTS (
        SELECT 1
        FROM jsonb_array_elements(s.value#>'{packages}') AS p
        WHERE p->>'registryType' = 'nuget' AND p->>'registryBaseUrl' IS NOT NULL
    );

COMMIT;
