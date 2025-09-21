-- Move status field from server.json to registry extensions
-- This migration restructures the data to separate immutable server.json from registry metadata

-- Update all server records to move status from server.json to _meta.io.modelcontextprotocol.registry/official
UPDATE servers
SET value = (
    -- Remove status from the root server.json
    value - 'status' ||
    -- Add status to the registry extensions if it exists in the original
    CASE
        WHEN value ? 'status' AND value ? '_meta' AND value->'_meta' ? 'io.modelcontextprotocol.registry/official' THEN
            jsonb_set(
                value - 'status',
                '{_meta,io.modelcontextprotocol.registry/official,status}',
                value->'status'
            )
        WHEN value ? 'status' AND value ? '_meta' AND NOT (value->'_meta' ? 'io.modelcontextprotocol.registry/official') THEN
            jsonb_set(
                value - 'status',
                '{_meta,io.modelcontextprotocol.registry/official}',
                jsonb_build_object('status', value->'status')
            )
        WHEN value ? 'status' AND NOT (value ? '_meta') THEN
            jsonb_set(
                value - 'status',
                '{_meta}',
                jsonb_build_object('io.modelcontextprotocol.registry/official', jsonb_build_object('status', value->'status'))
            )
        ELSE
            value - 'status'  -- Remove status even if no official extensions exist yet
    END
)
WHERE value IS NOT NULL;

-- Ensure all records have a default status in registry extensions if none was present
UPDATE servers
SET value = jsonb_set(
    value,
    '{_meta,io.modelcontextprotocol.registry/official,status}',
    '"active"'
)
WHERE value IS NOT NULL
  AND (
    NOT (value ? '_meta') OR
    NOT (value->'_meta' ? 'io.modelcontextprotocol.registry/official') OR
    NOT (value->'_meta'->'io.modelcontextprotocol.registry/official' ? 'status')
  );