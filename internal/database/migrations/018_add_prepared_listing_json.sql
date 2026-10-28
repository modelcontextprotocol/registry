-- Derived, versioned HTTP representation. Nullable columns avoid a table rewrite
-- and let existing rows use the ordinary typed normalization until backfilled.
SET LOCAL lock_timeout = '5s';
ALTER TABLE servers ADD COLUMN listing_value bytea;
ALTER TABLE servers ADD COLUMN listing_codec integer;

-- Older binaries update only value. Invalidate their derived bytes immediately
-- so rolling deploys and rollback cannot serve stale payloads. Metadata is read
-- separately on every request and does not require invalidation.
CREATE FUNCTION invalidate_listing_json() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.value IS DISTINCT FROM OLD.value
       AND NEW.listing_value IS NOT DISTINCT FROM OLD.listing_value
       AND NEW.listing_codec IS NOT DISTINCT FROM OLD.listing_codec THEN
        NEW.listing_value := NULL;
        NEW.listing_codec := NULL;
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER invalidate_listing_json_before_update
BEFORE UPDATE OF value ON servers
FOR EACH ROW EXECUTE FUNCTION invalidate_listing_json();
