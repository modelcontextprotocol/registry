-- Migrate server.json fields from snake_case to camelCase
-- This migration converts all fields including those in _meta objects

-- Helper function to recursively convert snake_case keys to camelCase in JSON objects
-- This handles nested structures like arguments, environment variables, etc.
CREATE OR REPLACE FUNCTION convert_object_keys_to_camelcase(input_json jsonb)
RETURNS jsonb
LANGUAGE plpgsql
AS $$
DECLARE
    result jsonb := '{}';
    key text;
    value jsonb;
    new_key text;
BEGIN
    -- Handle null input
    IF input_json IS NULL THEN
        RETURN NULL;
    END IF;

    -- Iterate through all keys in the object
    FOR key, value IN SELECT * FROM jsonb_each(input_json)
    LOOP
        -- Convert snake_case keys to camelCase
        new_key := CASE
            WHEN key = 'registry_type' THEN 'registryType'
            WHEN key = 'registry_base_url' THEN 'registryBaseUrl'
            WHEN key = 'file_sha256' THEN 'fileSha256'
            WHEN key = 'runtime_hint' THEN 'runtimeHint'
            WHEN key = 'runtime_arguments' THEN 'runtimeArguments'
            WHEN key = 'package_arguments' THEN 'packageArguments'
            WHEN key = 'environment_variables' THEN 'environmentVariables'
            WHEN key = 'is_required' THEN 'isRequired'
            WHEN key = 'is_secret' THEN 'isSecret'
            WHEN key = 'value_hint' THEN 'valueHint'
            WHEN key = 'is_repeated' THEN 'isRepeated'
            WHEN key = 'website_url' THEN 'websiteUrl'
            -- _meta specific fields
            WHEN key = 'is_latest' THEN 'isLatest'
            WHEN key = 'published_at' THEN 'publishedAt'
            WHEN key = 'updated_at' THEN 'updatedAt'
            ELSE key  -- Keep other keys unchanged
        END;

        -- Process values based on their type
        IF jsonb_typeof(value) = 'array' THEN
            -- Process array elements
            result := jsonb_set(result, ARRAY[new_key],
                (SELECT jsonb_agg(
                    CASE
                        WHEN jsonb_typeof(elem) = 'object' THEN convert_object_keys_to_camelcase(elem)
                        ELSE elem
                    END
                ) FROM jsonb_array_elements(value) AS elem)
            );
        ELSIF jsonb_typeof(value) = 'object' THEN
            -- Process nested objects
            result := jsonb_set(result, ARRAY[new_key], convert_object_keys_to_camelcase(value));
        ELSE
            -- Keep all primitive values (strings, numbers, booleans, null) unchanged
            result := jsonb_set(result, ARRAY[new_key], value);
        END IF;
    END LOOP;

    RETURN result;
END;
$$;

-- Update all server records to use camelCase field names
-- This converts snake_case keys in all fields including _meta sections
UPDATE servers
SET value = convert_object_keys_to_camelcase(value)
WHERE value IS NOT NULL;

-- Update $schema field to latest version if it exists
UPDATE servers
SET value = jsonb_set(value, '{$schema}', '"https://static.modelcontextprotocol.io/schemas/2025-09-16/server.schema.json"')
WHERE value ? '$schema' AND value IS NOT NULL;

-- Clean up the helper function
DROP FUNCTION IF EXISTS convert_object_keys_to_camelcase(jsonb);

