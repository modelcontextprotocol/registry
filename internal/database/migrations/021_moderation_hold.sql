-- Every existing version starts without a hold. Verified historical takedowns
-- are reconciled separately using the operator's records.
SET LOCAL lock_timeout = '2s';
ALTER TABLE servers
    ADD COLUMN moderation_hold boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT moderation_hold_requires_deletion
        CHECK (NOT moderation_hold OR status = 'deleted') NOT VALID;
-- Existing rows satisfy this by default. NOT VALID avoids an exclusive-lock
-- table scan and PostgreSQL enforces the constraint on every subsequent write.
