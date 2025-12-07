using System.Text.Json.Serialization;

namespace McpRegistrySearch.Models;

public class ServerResponse
{
    public Server? Server { get; set; }
    
    // Track which registry this result came from
    [JsonIgnore]
    public string? RegistrySource { get; set; }
}

public class SearchResultsResponse
{
    [JsonPropertyName("servers")]
    public List<ServerResponse>? Servers { get; set; }
}

public class Server
{
    [JsonPropertyName("$schema")]
    public string? Schema { get; set; }
    
    public string? Name { get; set; }
    public string? Description { get; set; }
    public string? Version { get; set; }
    
    [JsonPropertyName("websiteUrl")]
    public string? WebsiteUrl { get; set; }
    
    public Repository? Repository { get; set; }
    public List<Package>? Packages { get; set; }
    public List<Remote>? Remotes { get; set; }
    
    // [JsonPropertyName("_meta")]
    // public Dictionary<string, object>? Meta { get; set; }
}

public class Repository
{
    public string? Url { get; set; }
    public string? Source { get; set; }
    public string? Id { get; set; }
    public string? Subfolder { get; set; }
}

public class Package
{
    [JsonPropertyName("registryType")]
    public string? RegistryType { get; set; }
    
    [JsonPropertyName("registryBaseUrl")]
    public string? RegistryBaseUrl { get; set; }
    
    public string? Identifier { get; set; }
    public string? Version { get; set; }
    
    [JsonPropertyName("fileSha256")]
    public string? FileSha256 { get; set; }
    
    [JsonPropertyName("runtimeHint")]
    public string? RuntimeHint { get; set; }
    
    public Transport? Transport { get; set; }
    
    [JsonPropertyName("runtimeArguments")]
    public List<Argument>? RuntimeArguments { get; set; }
    
    [JsonPropertyName("packageArguments")]
    public List<Argument>? PackageArguments { get; set; }
    
    [JsonPropertyName("environmentVariables")]
    public List<EnvironmentVariable>? EnvironmentVariables { get; set; }
}

public class Transport
{
    public string? Type { get; set; }
    public string? Url { get; set; }
    public List<Header>? Headers { get; set; }
}

public class Header
{
    public string? Name { get; set; }
    public string? Value { get; set; }
    public string? Description { get; set; }
    
    [JsonPropertyName("isRequired")]
    public bool IsRequired { get; set; }
    
    [JsonPropertyName("isSecret")]
    public bool IsSecret { get; set; }
}

public class EnvironmentVariable
{
    public string? Name { get; set; }
    public string? Value { get; set; }
    public string? Description { get; set; }
    
    [JsonPropertyName("isRequired")]
    public bool IsRequired { get; set; }
    
    [JsonPropertyName("isSecret")]
    public bool IsSecret { get; set; }
    
    public string? Default { get; set; }
}

public class Argument
{
    public string? Type { get; set; }
    
    [JsonPropertyName("valueHint")]
    public string? ValueHint { get; set; }
    
    public string? Name { get; set; }
    
    [JsonPropertyName("isRepeated")]
    public bool IsRepeated { get; set; }
    
    public string? Description { get; set; }
    public string? Value { get; set; }
    
    [JsonPropertyName("isRequired")]
    public bool IsRequired { get; set; }
    
    [JsonPropertyName("isSecret")]
    public bool IsSecret { get; set; }
}

public class Remote
{
    public string? Type { get; set; }
    public string? Url { get; set; }
    public List<Header>? Headers { get; set; }
}