# Azure Entra ID Authentication

This document explains how to set up Azure Entra ID (formerly Azure Active Directory) authentication for the MCP Registry.

## Overview

The Entra ID authentication endpoint (`/v0/auth/entra-id`) allows users and service principals to authenticate using Azure Entra ID tokens. This is particularly useful for:

- **Azure DevOps/GitHub Actions pipelines** using Azure service connections
- **Managed identities** in Azure
- **Service principals** for automated publishing
- **User authentication** via Azure AD

## Azure Setup

### 1. Create an App Registration

1. Go to the [Azure Portal](https://portal.azure.com)
2. Navigate to **Azure Active Directory** → **App registrations**
3. Click **New registration**
4. Configure:
   - **Name**: `MCP Registry` (or your preferred name)
   - **Supported account types**: Choose based on your needs
     - Single tenant: Only your organization
     - Multi-tenant: Any Azure AD directory
   - **Redirect URI**: Not required for this use case
5. Click **Register**

### 2. Configure the Application

After creating the app registration:

1. Note down the following values (you'll need them for configuration):
   - **Application (client) ID**: Found on the Overview page
   - **Directory (tenant) ID**: Found on the Overview page

2. **Create a client secret** (if using service principals):
   - Go to **Certificates & secrets**
   - Click **New client secret**
   - Add a description and set expiration
   - **Copy the secret value immediately** (it won't be shown again)

3. **Configure API permissions** (optional, if you want to validate specific permissions):
   - Go to **API permissions**
   - Add any required Microsoft Graph or custom API permissions
   - Grant admin consent if required

### 3. Configure Token Settings

1. Go to **Token configuration**
2. Add optional claims if needed (e.g., email, preferred_username)
3. Configure **Audience**: The client ID from step 2

## Registry Configuration

Configure the MCP Registry with the following environment variables:

```bash
# Enable Entra ID authentication
MCP_REGISTRY_ENTRA_ID_ENABLED=true

# Your Azure AD tenant ID (from App Registration overview)
MCP_REGISTRY_ENTRA_ID_TENANT_ID=00000000-0000-0000-0000-000000000000

# Your App Registration client ID
MCP_REGISTRY_ENTRA_ID_CLIENT_ID=11111111-1111-1111-1111-111111111111

# Namespace pattern (optional, see below)
MCP_REGISTRY_ENTRA_ID_NAMESPACE_PATTERN=com.{reversed_domain}.*

# Simple namespace for compatibility with simple server names (optional, see below)
MCP_REGISTRY_ENTRA_ID_SIMPLE_NAMESPACE={company}/*

# Allow edit permissions (optional, default: false)
MCP_REGISTRY_ENTRA_ID_ALLOW_EDIT=true
```

### Namespace Pattern Configuration

The registry supports **two namespace formats** to accommodate different server naming conventions:

#### 1. Full Reverse-DNS Pattern (`ENTRA_ID_NAMESPACE_PATTERN`)

Used for fully-qualified server names in reverse-DNS format.

**Placeholders:**
- `{tenant_id}`: The Azure AD tenant ID
- `{app_id}`: The application (client) ID from the token
- `{domain}`: The domain extracted from preferred_username (e.g., `contoso.com`)
- `{reversed_domain}`: Reversed domain format (e.g., `com.contoso`)

**Examples:**

```bash
# Allow publishing to com.contoso.* for user@contoso.com
ENTRA_ID_NAMESPACE_PATTERN=com.{reversed_domain}.*
# Allows: com.contoso.my-server, com.contoso.another-server

# Allow publishing to tenant-specific namespace
ENTRA_ID_NAMESPACE_PATTERN=com.microsoft.entra.{tenant_id}.*
# Allows: com.microsoft.entra.00000000-0000-0000-0000-000000000000.my-server

# Custom pattern combining multiple elements
ENTRA_ID_NAMESPACE_PATTERN=io.azure.{domain}.*
# Allows: io.azure.contoso.com.my-server
```

#### 2. Simple Namespace (`ENTRA_ID_SIMPLE_NAMESPACE`)

Used for simple server names like `microsoft/azure-devops-mcp` or `contoso/my-server`.

**Placeholders:**
- `{company}`: Company name extracted from domain (e.g., `microsoft` from `user@microsoft.com`)
- `{domain}`: Full domain (e.g., `contoso.com`)
- `{app_name}`: Application display name (cleaned, lowercase, dash-separated)
- `{tenant_id}`: Azure AD tenant ID
- `{app_id}`: Application (client) ID

**Examples:**

```bash
# Allow publishing to company/* for user@microsoft.com
ENTRA_ID_SIMPLE_NAMESPACE={company}/*
# Allows: microsoft/server-name, microsoft/another-server

# For service principals with app display name "Azure DevOps"
ENTRA_ID_SIMPLE_NAMESPACE={company}/{app_name}/*
# Allows: microsoft/azure-devops/my-server

# Mixed format
ENTRA_ID_SIMPLE_NAMESPACE=azure/{company}/*
# Allows: azure/contoso/server-name
```

**Auto-extraction (if not configured):**
If `ENTRA_ID_SIMPLE_NAMESPACE` is not set, the system auto-extracts from the reverse-DNS pattern:
- `com.microsoft.*` → `microsoft/*`
- `io.github.username.*` → `io.github.username/*`

#### Combined Example

For user `user@microsoft.com`:

```bash
# Full configuration
ENTRA_ID_NAMESPACE_PATTERN=com.{reversed_domain}.*
ENTRA_ID_SIMPLE_NAMESPACE={company}/*
```

**This allows publishing servers with EITHER naming format:**
- ✅ `com.microsoft.azure-devops` (full reverse-DNS)
- ✅ `microsoft/azure-devops-mcp` (simple format)

**Default behavior** (if not configured):
- Full pattern: `com.microsoft.entra.{tenant_id}.*`
- Simple pattern: Auto-extracted from full pattern

## Usage Examples

### 1. Using Service Principal (Azure Pipeline)

```yaml
# azure-pipelines.yml
trigger:
  - main

pool:
  vmImage: 'ubuntu-latest'

steps:
  - task: AzureCLI@2
    displayName: 'Get Azure Token and Publish'
    inputs:
      azureSubscription: 'your-service-connection'
      scriptType: 'bash'
      scriptLocation: 'inlineScript'
      inlineScript: |
        # Get an access token for the App Registration
        TOKEN=$(az account get-access-token \
          --resource 11111111-1111-1111-1111-111111111111 \
          --query accessToken -o tsv)
        
        # Exchange for registry token
        REGISTRY_TOKEN=$(curl -s -X POST \
          https://registry.modelcontextprotocol.io/v0/auth/entra-id \
          -H "Content-Type: application/json" \
          -d "{\"access_token\": \"$TOKEN\"}" \
          | jq -r '.registry_token')
        
        # Publish server
        curl -X POST \
          https://registry.modelcontextprotocol.io/v0/publish \
          -H "Content-Type: application/json" \
          -H "Authorization: Bearer $REGISTRY_TOKEN" \
          -d @server.json
```

### 2. Using User Authentication (Interactive)

```bash
#!/bin/bash

# Login to Azure (interactive)
az login

# Get access token
TOKEN=$(az account get-access-token \
  --resource 11111111-1111-1111-1111-111111111111 \
  --query accessToken -o tsv)

# Exchange for registry token
REGISTRY_TOKEN=$(curl -s -X POST \
  https://registry.modelcontextprotocol.io/v0/auth/entra-id \
  -H "Content-Type: application/json" \
  -d "{\"access_token\": \"$TOKEN\"}" \
  | jq -r '.registry_token')

# Publish server
curl -X POST \
  https://registry.modelcontextprotocol.io/v0/publish \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $REGISTRY_TOKEN" \
  -d @server.json
```

### 3. Using Managed Identity (Azure VM/Container)

```bash
#!/bin/bash

# Get token from managed identity endpoint
TOKEN=$(curl -s 'http://169.254.169.254/metadata/identity/oauth2/token?api-version=2018-02-01&resource=11111111-1111-1111-1111-111111111111' \
  -H Metadata:true \
  | jq -r '.access_token')

# Exchange for registry token
REGISTRY_TOKEN=$(curl -s -X POST \
  https://registry.modelcontextprotocol.io/v0/auth/entra-id \
  -H "Content-Type: application/json" \
  -d "{\"access_token\": \"$TOKEN\"}" \
  | jq -r '.registry_token')

# Publish
curl -X POST \
  https://registry.modelcontextprotocol.io/v0/publish \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $REGISTRY_TOKEN" \
  -d @server.json
```

### 4. Using GitHub Actions with Azure Login

```yaml
name: Publish to MCP Registry

on:
  push:
    tags:
      - 'v*'

jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Azure Login
        uses: azure/login@v1
        with:
          creds: ${{ secrets.AZURE_CREDENTIALS }}
      
      - name: Get Azure Token
        id: azure-token
        run: |
          TOKEN=$(az account get-access-token \
            --resource 11111111-1111-1111-1111-111111111111 \
            --query accessToken -o tsv)
          echo "::add-mask::$TOKEN"
          echo "token=$TOKEN" >> $GITHUB_OUTPUT
      
      - name: Publish to Registry
        env:
          AZURE_TOKEN: ${{ steps.azure-token.outputs.token }}
        run: |
          # Exchange for registry token
          REGISTRY_TOKEN=$(curl -s -X POST \
            https://registry.modelcontextprotocol.io/v0/auth/entra-id \
            -H "Content-Type: application/json" \
            -d "{\"access_token\": \"$AZURE_TOKEN\"}" \
            | jq -r '.registry_token')
          
          # Publish
          curl -X POST \
            https://registry.modelcontextprotocol.io/v0/publish \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer $REGISTRY_TOKEN" \
            -d @server.json
```

## Token Types

The endpoint accepts both **ID tokens** and **access tokens** from Azure Entra ID:

### ID Token (Recommended)
- Contains user/app identity claims
- Includes email, name, preferred_username
- Best for user authentication

### Access Token
- Used for API authorization
- Contains app-only or delegated permissions
- Best for service principals and managed identities

## Permissions

The registry token issued after authentication includes:

1. **Publish permission**: Always granted for the configured namespace pattern
2. **Edit permission**: Only granted if `ENTRA_ID_ALLOW_EDIT=true`

## Troubleshooting

### Token Validation Failed

**Error**: `Invalid Entra ID token`

**Solutions**:
- Verify the token is for the correct audience (client ID)
- Check that the token hasn't expired
- Ensure the tenant ID matches the configuration
- Verify the App Registration is configured correctly

### Wrong Tenant

**Error**: `token is from unexpected tenant`

**Solutions**:
- Verify `ENTRA_ID_TENANT_ID` matches your Azure AD tenant
- Check that the user/service principal belongs to the correct tenant

### Missing Claims

**Error**: Claims not being extracted properly

**Solutions**:
- Configure optional claims in App Registration → Token configuration
- Request an ID token instead of access token
- Check that scopes include `openid`, `profile`, `email`

## Security Considerations

1. **Token Storage**: Never commit tokens or client secrets to version control
2. **Token Expiration**: Tokens are short-lived; refresh them as needed
3. **Principle of Least Privilege**: Configure namespace patterns to limit what can be published
4. **Audit Logging**: Monitor authentication attempts and publishes
5. **Client Secret Rotation**: Regularly rotate client secrets
6. **Multi-factor Authentication**: Enable MFA for user accounts

## API Reference

### Endpoint

```
POST /v0/auth/entra-id
```

### Request

```json
{
  "access_token": "eyJ0eXAiOiJKV1QiLCJhbGc..."
}
```

### Response (Success)

```json
{
  "registry_token": "eyJhbGciOiJFZERTQSIsInR5cCI6IkpXVCJ9..."
}
```

### Response (Error)

```json
{
  "status": 401,
  "title": "Unauthorized",
  "detail": "Invalid Entra ID token: failed to verify ID token: ..."
}
```

## Further Reading

- [Azure AD App Registration Documentation](https://learn.microsoft.com/en-us/azure/active-directory/develop/quickstart-register-app)
- [Azure Managed Identity](https://learn.microsoft.com/en-us/azure/active-directory/managed-identities-azure-resources/overview)
- [Azure DevOps Service Connections](https://learn.microsoft.com/en-us/azure/devops/pipelines/library/service-endpoints)
- [GitHub Actions Azure Login](https://github.com/Azure/login)
