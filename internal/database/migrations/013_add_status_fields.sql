-- Add status management fields for deprecation features
-- Status values: active, deprecated, deleted

BEGIN;

-- Add new columns for status management
ALTER TABLE servers ADD COLUMN status_changed_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE servers ADD COLUMN status_message TEXT;

-- Constraint: status_message must not exceed 500 characters
ALTER TABLE servers ADD CONSTRAINT check_status_message_length
    CHECK (length(status_message) <= 500);

-- Initialize status_changed_at with published_at for existing records
UPDATE servers SET status_changed_at = published_at WHERE status_changed_at IS NULL;

-- Make status_changed_at NOT NULL now that all records have values
ALTER TABLE servers ALTER COLUMN status_changed_at SET NOT NULL;

-- Constraint: status_changed_at must be >= published_at
ALTER TABLE servers ADD CONSTRAINT check_status_changed_at_after_published
    CHECK (status_changed_at >= published_at);

COMMIT;
