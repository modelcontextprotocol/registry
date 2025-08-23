-- PostgreSQL schema for MCP Registry
-- This schema uses a hybrid approach with relational tables for core entities
-- and JSONB columns for complex nested data structures

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";

-- Servers table - core server information
CREATE TABLE servers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    status VARCHAR(50) DEFAULT 'active',
    
    -- Repository information stored as JSONB for flexibility
    repository JSONB,
    
    -- Version details
    version VARCHAR(255) NOT NULL,
    release_date TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    is_latest BOOLEAN DEFAULT true,
    
    -- Packages and remotes stored as JSONB arrays
    packages JSONB DEFAULT '[]'::jsonb,
    remotes JSONB DEFAULT '[]'::jsonb,
    
    -- Metadata
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Indexes for performance
CREATE UNIQUE INDEX idx_servers_id ON servers(id);
CREATE INDEX idx_servers_name ON servers(name);
CREATE UNIQUE INDEX idx_servers_name_version ON servers(name, version);
CREATE INDEX idx_servers_latest ON servers(is_latest) WHERE is_latest = true;
CREATE INDEX idx_servers_status ON servers(status);

-- GIN indexes for JSONB fields
CREATE INDEX idx_servers_repository_gin ON servers USING GIN(repository);
CREATE INDEX idx_servers_packages_gin ON servers USING GIN(packages);
CREATE INDEX idx_servers_remotes_gin ON servers USING GIN(remotes);

-- Function to update the updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Trigger to automatically update updated_at
CREATE TRIGGER update_servers_updated_at 
    BEFORE UPDATE ON servers 
    FOR EACH ROW 
    EXECUTE FUNCTION update_updated_at_column();
