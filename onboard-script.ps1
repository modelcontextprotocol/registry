<#
.SYNOPSIS
    Publish an MCP server JSON to your internal registry using an existing Entra ID token

.EXAMPLE
    .\onboard-script.ps1 -EntraIdToken $token -ServerJsonPath ".\server.json"
#>

param(
    [Parameter(Mandatory=$true)]
    [string]$EntraIdToken,
    
    [Parameter(Mandatory=$true)]
    [string]$ServerJsonPath,
    
    [string]$InternalRegistry = "https://mcp-registry-app-tst.proudisland-2110e9aa.westeurope.azurecontainerapps.io/"
)

function Write-Step { Write-Host "▶ $args" -ForegroundColor Cyan }
function Write-Success { Write-Host "✅ $args" -ForegroundColor Green }
function Write-Error { Write-Host "❌ $args" -ForegroundColor Red }

try {
    # Validate server.json file exists
    Write-Step "Validating server JSON file..."
    if (-not (Test-Path -Path $ServerJsonPath)) {
        Write-Error "Server JSON file not found: $ServerJsonPath"
        exit 1
    }
    
    $serverJson = Get-Content -Path $ServerJsonPath -Raw
    Write-Success "Server JSON file validated"
    
    # Login to internal MCP registry
    Write-Step "Getting registry token from internal MCP registry..."
    $authResponse = Invoke-RestMethod -Uri "$InternalRegistry/v0.1/auth/entra-id" `
        -Method Post -ContentType "application/json" `
        -Body (@{access_token = $EntraIdToken} | ConvertTo-Json)
    
    Write-Success "Successfully authenticated with internal registry"
    
    # Publish server JSON
    Write-Step "Publishing server to internal registry..."
    Invoke-RestMethod -Uri "$InternalRegistry/v0.1/publish" `
        -Method Post `
        -Headers @{Authorization = "Bearer $($authResponse.registry_token)"} `
        -ContentType "application/json" `
        -Body $serverJson
    
    Write-Success "Successfully published server to internal registry!"
} catch {
    Write-Error "Failed: $($_)"
    exit 1
}
