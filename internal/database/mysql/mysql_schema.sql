-- Create the servers table if it doesn't exist
CREATE TABLE IF NOT EXISTS servers (
    id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    repository_url VARCHAR(255) NOT NULL,
    repository_source VARCHAR(50) NOT NULL,
    repository_id VARCHAR(255) NOT NULL,
    version_detail_version VARCHAR(50) NOT NULL,
    version_detail_release_date VARCHAR(50) NOT NULL,
    version_detail_is_latest BOOLEAN NOT NULL DEFAULT TRUE,
    packages JSON,
    remotes JSON,
    INDEX idx_name_version (name, version_detail_version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci; 