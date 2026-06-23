# 測試 MCP Registry API 端點 (PowerShell 版本)

param(
    [string]$BaseUrl = "http://localhost:8081"
)

function Test-Endpoint {
    param(
        [string]$Name,
        [string]$Method = "GET",
        [string]$Url,
        [object]$Body = $null,
        [int]$ExpectedStatus = 200
    )
    
    Write-Host "`n🧪 測試: $Name" -ForegroundColor Blue
    Write-Host "$Method $Url" -ForegroundColor Gray
    
    try {
        $params = @{
            Uri = $Url
            Method = $Method
            ErrorAction = 'Stop'
        }
        
        if ($Body) {
            $params.Body = ($Body | ConvertTo-Json -Depth 10)
            $params.ContentType = 'application/json'
        }
        
        $response = Invoke-RestMethod @params
        Write-Host "✓ 成功" -ForegroundColor Green
        
        # 顯示回應
        if ($response -is [string]) {
            Write-Host $response
        } else {
            $response | ConvertTo-Json -Depth 5 | Write-Host
        }
        
        return $true
    }
    catch {
        $statusCode = $_.Exception.Response.StatusCode.Value__
        if ($statusCode -eq $ExpectedStatus -and $ExpectedStatus -ne 200) {
            Write-Host "✓ 成功 - 正確返回 HTTP $statusCode" -ForegroundColor Green
            return $true
        } else {
            Write-Host "✗ 失敗 - $($_.Exception.Message)" -ForegroundColor Red
            return $false
        }
    }
}

Write-Host "========================" -ForegroundColor Cyan
Write-Host "🧪 測試 MCP Registry API" -ForegroundColor Cyan
Write-Host "========================" -ForegroundColor Cyan
Write-Host "Base URL: $BaseUrl" -ForegroundColor Yellow
Write-Host ""

# Test 1: Health Check
Test-Endpoint -Name "Health Check" -Url "$BaseUrl/healthz"

# Test 2: Ping
Test-Endpoint -Name "Ping" -Url "$BaseUrl/v0.1/ping"

# Test 3: List All Servers
$result = Test-Endpoint -Name "List All Servers" -Url "$BaseUrl/v0.1/servers"

# Test 4: Get Figma MCP Server (latest)
Test-Endpoint -Name "Get Figma MCP Server (latest)" `
    -Url "$BaseUrl/v0.1/servers/io.figma%2Fmcp-server/versions/latest"

# Test 5: Get Figma MCP Server (v1.0.0)
Test-Endpoint -Name "Get Figma MCP Server (v1.0.0)" `
    -Url "$BaseUrl/v0.1/servers/io.figma%2Fmcp-server/versions/1.0.0"

# Test 6: Get Airtable MCP Server (latest)
Test-Endpoint -Name "Get Airtable MCP Server (latest)" `
    -Url "$BaseUrl/v0.1/servers/io.github.domdomegg%2Fairtable-mcp-server/versions/latest"

# Test 7: POST Create Server
$testServer = @{
    name = "com.example/test-server"
    description = "Test MCP Server"
    version = "1.0.0"
    repository = @{
        url = "https://github.com/example/test-server"
        source = "github"
    }
}

Test-Endpoint -Name "POST Create Server" `
    -Method "POST" `
    -Url "$BaseUrl/v0.1/servers" `
    -Body $testServer

# Test 8: Not Found (404)
Write-Host "`n🧪 測試: Get Non-existent Server (應返回 404)" -ForegroundColor Blue
Write-Host "GET $BaseUrl/v0.1/servers/nonexistent.server/versions/latest" -ForegroundColor Gray
try {
    Invoke-RestMethod -Uri "$BaseUrl/v0.1/servers/nonexistent.server/versions/latest" -ErrorAction Stop
    Write-Host "✗ 失敗 - 應該返回 404" -ForegroundColor Red
}
catch {
    $statusCode = $_.Exception.Response.StatusCode.Value__
    if ($statusCode -eq 404) {
        Write-Host "✓ 成功 - 正確返回 404" -ForegroundColor Green
    } else {
        Write-Host "✗ 失敗 - HTTP Code: $statusCode (期望 404)" -ForegroundColor Red
    }
}

Write-Host "`n========================" -ForegroundColor Cyan
Write-Host "✅ 測試完成" -ForegroundColor Green
Write-Host "========================" -ForegroundColor Cyan
