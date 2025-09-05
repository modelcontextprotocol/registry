# MCP Registry Technical Architecture

This document describes the technical architecture of the MCP Registry, including system components, deployment strategies, and data flows.

## System Overview

The MCP Registry is designed as a lightweight metadata service that bridges MCP server creators with consumers (MCP clients and aggregators).

## Core Components

### REST API (Go)

The main application server implemented in Go, providing:
- Public read endpoints for server discovery
- Authenticated write endpoints for server publication
- GitHub OAuth integration (extensible to other providers)
- DNS verification system (optional for custom namespaces)

### Database (PostgreSQL)

Primary data store for:
- Versioned server metadata (server.json contents)
- User authentication state
- DNS verification records

### CDN Layer

Critical for scalability:
- Caches all public read endpoints
- Reduces load on origin servers
- Enables global distribution
- Designed for daily consumer polling patterns

### CLI Tool

Developer interface for:
- Server publication workflow
- GitHub OAuth flow
- DNS verification

## Deployment Architecture

### Kubernetes Deployment (Helm)

The registry is designed to run on Kubernetes using Helm charts:

```mermaid
graph TB
    subgraph "Kubernetes Cluster"
        subgraph "Namespace: mcp-registry"
            subgraph "Registry Service"
                LB[Load Balancer<br/>:80]
                RS[Registry Service<br/>:8080]
                RP1[Registry Pod 1]
                RP2[Registry Pod 2]
                RP3[Registry Pod N]
            end
            
            subgraph "Database Service"
                DBS[DB Service<br/>:27017]
                SS[StatefulSet]
                PV[Persistent Volume]
            end
            
            subgraph "Secrets"
                GHS[GitHub OAuth Secret]
            end
        end
    end
    
    LB --> RS
    RS --> RP1
    RS --> RP2
    RS --> RP3
    RP1 --> DBS
    RP2 --> DBS
    RP3 --> DBS
    DBS --> SS
    SS --> PV
    RP1 -.-> GHS
    RP2 -.-> GHS
    RP3 -.-> GHS
```

## Data Flow Patterns

### 1. Server Publication Flow

```mermaid
sequenceDiagram
    participant Dev as Developer
    participant CLI as CLI Tool
    participant API as Registry API
    participant DB as Database
    participant GH as GitHub
    participant DNS as DNS Provider
    
    Dev->>CLI: mcp publish server.json
    CLI->>CLI: Validate server.json
    CLI->>GH: OAuth flow
    GH-->>CLI: Access token
    CLI->>API: POST /servers
    API->>GH: Verify token
    API->>DNS: Verify domain (if applicable)
    API->>DB: Store metadata
    API-->>CLI: Success
    CLI-->>Dev: Published!
```

### 2. Consumer Discovery Flow

```mermaid
sequenceDiagram
    participant Client as MCP Client Host App
    participant INT as Intermediary<br/>(Marketplace/Aggregator)
    participant CDN as CDN Cache
    participant API as Registry API
    participant DB as Database
    
    Note over INT,CDN: Daily ETL Process
    INT->>CDN: GET /servers
    alt Cache Hit
        CDN-->>INT: Cached response
    else Cache Miss
        CDN->>API: GET /servers
        API->>DB: Query servers
        DB-->>API: Server list
        API-->>CDN: Response + cache headers
        CDN-->>INT: Response
    end
    INT->>INT: Process & enhance data
    INT->>INT: Store in local cache
    
    Note over Client,INT: Real-time Client Access
    Client->>INT: Request server list
    INT-->>Client: Curated/enhanced data
```

### 3. DNS Verification Flow

```mermaid
sequenceDiagram
    participant User as User
    participant CLI as CLI Tool
    participant API as Registry API
    participant DNS as DNS Provider
    participant DB as Database
    
    User->>CLI: mcp verify-domain example.com
    CLI->>API: POST /verify-domain
    API->>API: Generate verification token
    API->>DB: Store pending verification
    API-->>CLI: TXT record: mcp-verify=abc123
    CLI-->>User: Add TXT record to DNS
    User->>DNS: Configure TXT record
    User->>CLI: Confirm added
    CLI->>API: POST /verify-domain/check
    API->>DNS: Query TXT records
    DNS-->>API: TXT records
    API->>API: Validate token
    API->>DB: Store verification
    API-->>CLI: Domain verified
    CLI-->>User: Success!
```

### 4. Admin OIDC Authentication Flow

For registry administration, users with @modelcontextprotocol.io Google Cloud Identity accounts can authenticate using OIDC:

```mermaid
sequenceDiagram
    participant Admin as Admin User
    participant CLI as Admin CLI
    participant GCP as Google Cloud Identity
    participant API as Registry API
    
    Admin->>CLI: Request admin token
    CLI->>GCP: gcloud auth print-identity-token --audiences=mcp-registry
    GCP-->>CLI: ID Token (with hd: "modelcontextprotocol.io")
    CLI->>API: POST /v0/auth/oidc {"oidc_token": "eyJ..."}
    API->>GCP: Verify token signature (JWKS)
    API->>API: Validate claims (issuer, audience, hd)
    API->>API: Grant admin permissions (edit: *, publish: *)
    API-->>CLI: Registry JWT Token
    CLI->>API: POST /admin/* (with Registry JWT)
    API->>API: Validate JWT + permissions
    API-->>CLI: Admin operation success
```

**Usage:**
```bash
# Get Google Cloud Identity token
ID_TOKEN=$(gcloud auth print-identity-token)

# Exchange for Registry JWT token  
REGISTRY_TOKEN=$(curl -X POST /v0/auth/oidc \
  -H "Content-Type: application/json" \
  -d '{"oidc_token": "'$ID_TOKEN'"}' | jq -r .registry_token)

# Use for admin operations
curl -H "Authorization: Bearer $REGISTRY_TOKEN" /v0/...
```