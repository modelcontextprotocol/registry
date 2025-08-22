-- Add server_extensions table for registry metadata and publisher extensions
-- This migration implements the extension wrapper architecture

-- Create server_extensions table for registry/publisher metadata
CREATE TABLE server_extensions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    server_id UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    
    -- Registry metadata as structured columns
    published_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    is_latest BOOLEAN NOT NULL DEFAULT true,
    release_date TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    
    -- Publisher extensions as flexible JSONB
    publisher_extensions JSONB DEFAULT '{}'::jsonb,
    
    -- Ensure one extension record per server
    UNIQUE(server_id)
);

-- Indexes for performance
CREATE UNIQUE INDEX idx_server_extensions_server_id ON server_extensions(server_id);
CREATE INDEX idx_server_extensions_latest ON server_extensions(is_latest) WHERE is_latest = true;
CREATE INDEX idx_server_extensions_published_at ON server_extensions(published_at);

-- GIN index for publisher extensions JSONB field
CREATE INDEX idx_server_extensions_publisher_gin ON server_extensions USING GIN(publisher_extensions);

-- Migrate existing data from servers table to server_extensions table
INSERT INTO server_extensions (server_id, published_at, updated_at, is_latest, release_date, publisher_extensions)
SELECT 
    id,
    created_at, -- Use created_at as published_at
    updated_at,
    is_latest,
    release_date,
    '{}'::jsonb -- Empty publisher extensions
FROM servers;

-- Remove registry metadata columns from servers table (they now live in server_extensions)
ALTER TABLE servers DROP COLUMN release_date;
ALTER TABLE servers DROP COLUMN is_latest;

-- Update trigger to also update server_extensions updated_at when servers are updated
CREATE OR REPLACE FUNCTION update_server_extensions_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    -- Update the corresponding server_extensions record
    UPDATE server_extensions 
    SET updated_at = NOW() 
    WHERE server_id = NEW.id;
    
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Trigger to automatically update server_extensions updated_at when servers change
CREATE TRIGGER update_server_extensions_on_server_update 
    AFTER UPDATE ON servers 
    FOR EACH ROW 
    EXECUTE FUNCTION update_server_extensions_updated_at();