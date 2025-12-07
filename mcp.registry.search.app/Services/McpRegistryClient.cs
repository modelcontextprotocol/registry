using System.Text.Json;
using McpRegistrySearch.Models;

namespace McpRegistrySearch.Services;

public class McpRegistryClient
{
    private static readonly List<(string Url, string Name)> DefaultRegistries = new()
    {
        ("https://registry.modelcontextprotocol.io", "Official MCP Registry"),
        ("https://api.mcp.github.com/", "GitHub Registry")
    };

    private readonly List<(string Url, string Name)> _registries;

    public McpRegistryClient()
    {
        _registries = DefaultRegistries;
    }

    public async Task<List<ServerResponse>> SearchServersAsync(string searchTerm)
    {
        var encodedSearchTerm = Uri.EscapeDataString(searchTerm);
        
        // Search all registries in parallel
        var tasks = _registries.Select(registry => 
            SearchSingleRegistryAsync(registry.Url, registry.Name, encodedSearchTerm)).ToList();
        
        var results = await Task.WhenAll(tasks);
        
        // Aggregate and deduplicate results
        var allServers = new List<ServerResponse>();
        var seenServers = new HashSet<string>(); // Track by name to avoid duplicates
        
        foreach (var result in results)
        {
            foreach (var serverResponse in result)
            {
                var serverName = serverResponse.Server?.Name;
                if (serverName != null && !seenServers.Contains(serverName))
                {
                    seenServers.Add(serverName);
                    allServers.Add(serverResponse);
                }
            }
        }
        
        return allServers.OrderBy(s => s.Server?.Name).ToList();
    }

    private async Task<List<ServerResponse>> SearchSingleRegistryAsync(string baseUrl, string registryName, string encodedSearchTerm)
    {
        try
        {
            using var httpClient = new HttpClient
            {
                BaseAddress = new Uri(baseUrl),
                Timeout = TimeSpan.FromSeconds(30)
            };
            httpClient.DefaultRequestHeaders.Add("User-Agent", "mcp-registry-search/1.0");

            var response = await httpClient.GetAsync($"/v0.1/servers?search={encodedSearchTerm}");

            if (!response.IsSuccessStatusCode)
            {
                return new List<ServerResponse>();
            }

            var json = await response.Content.ReadAsStringAsync();

            var wrapper = JsonSerializer.Deserialize<SearchResultsResponse>(json, new JsonSerializerOptions
            {
                PropertyNamingPolicy = JsonNamingPolicy.CamelCase,
                PropertyNameCaseInsensitive = true
            });

            var servers = wrapper?.Servers ?? new List<ServerResponse>();
            
            // Tag each result with its registry source
            foreach (var server in servers)
            {
                server.RegistrySource = registryName;
            }
            
            return servers;
        }
        catch (HttpRequestException)
        {
            return new List<ServerResponse>();
        }
        catch (JsonException)
        {
            return new List<ServerResponse>();
        }
    }

    public async Task<Server?> GetServerByNameAsync(string name)
    {
        var encodedName = Uri.EscapeDataString(name);
        
        // Try each registry in parallel
        var tasks = _registries.Select(registry => 
            GetServerFromSingleRegistryAsync(registry.Url, encodedName)).ToList();
        
        var results = await Task.WhenAll(tasks);
        
        // Return the first non-null result
        return results.FirstOrDefault(s => s != null);
    }

    private async Task<Server?> GetServerFromSingleRegistryAsync(string baseUrl, string encodedName)
    {
        try
        {
            using var httpClient = new HttpClient
            {
                BaseAddress = new Uri(baseUrl),
                Timeout = TimeSpan.FromSeconds(30)
            };
            httpClient.DefaultRequestHeaders.Add("User-Agent", "mcp-registry-search/1.0");

            var response = await httpClient.GetAsync($"/v0.1/servers/{encodedName}/versions/latest");
            if (!response.IsSuccessStatusCode)
                return null;
                
            var json = await response.Content.ReadAsStringAsync();
            var serverResponse = JsonSerializer.Deserialize<ServerResponse>(json, new JsonSerializerOptions
            {
                PropertyNamingPolicy = JsonNamingPolicy.CamelCase,
                PropertyNameCaseInsensitive = true
            });

            return serverResponse?.Server;
        }
        catch (HttpRequestException)
        {
            return null;
        }
        catch (JsonException)
        {
            return null;
        }
    }
}