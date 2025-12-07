using McpRegistrySearch.Services;
using System.Text.Json;
using McpRegistrySearch.Models;
using Spectre.Console;

if (args.Length == 0)
{
    AnsiConsole.MarkupLine("[bold cyan]MCP Registry Search Tool[/]");
    AnsiConsole.MarkupLine("[dim]Search and download MCP server definitions from multiple registries[/]");
    AnsiConsole.WriteLine();
    AnsiConsole.MarkupLine("[yellow]Usage:[/] mcp-registry-search <search-term> [output-file]");
    AnsiConsole.MarkupLine("[dim]Examples:[/]");
    AnsiConsole.MarkupLine("  [grey]mcp-registry-search azure[/]");
    AnsiConsole.MarkupLine("  [grey]mcp-registry-search microsoft/markitdown custom-output.json[/]");
    AnsiConsole.WriteLine();
    return 1;
}

// Parse arguments
var searchTerm = args[0];
string? outputFile = null;

for (int i = 1; i < args.Length; i++)
{
    if (!args[i].StartsWith("--") && outputFile == null)
    {
        outputFile = args[i];
    }
}

try
{
    AnsiConsole.Write(new Rule($"[cyan]Searching for '{searchTerm}'[/]").RuleStyle("grey").LeftJustified());
    AnsiConsole.WriteLine();

    var client = new McpRegistryClient();

    // Try direct lookup first (if it's a full name like "microsoft/markitdown")
    Server? selectedServer = null;

    if (searchTerm.Contains('/'))
    {
        selectedServer = await AnsiConsole.Status()
            .Spinner(Spinner.Known.Dots)
            .StartAsync($"Looking up [yellow]{searchTerm}[/]...", async ctx => 
            {
                return await client.GetServerByNameAsync(searchTerm);
            });

        if (selectedServer == null)
        {
            AnsiConsole.MarkupLine($"[red]✗[/] Server '{searchTerm.EscapeMarkup()}' not found");
            return 1;
        }
    }
    else
    {
        // Search for matching servers across all registries
        var matches = await AnsiConsole.Status()
            .Spinner(Spinner.Known.Dots)
            .StartAsync("Searching registries...", async ctx => 
            {
                return await client.SearchServersAsync(searchTerm);
            });

        if (matches == null || matches.Count == 0)
        {
            AnsiConsole.MarkupLine($"[red]✗[/] No servers found matching '{searchTerm.EscapeMarkup()}'");
            return 1;
        }

        if (matches.Count == 1)
        {
            // Only one match, use it directly
            AnsiConsole.MarkupLine($"[green]✓[/] Found 1 match from [cyan]{matches[0].RegistrySource}[/]");
            selectedServer = await client.GetServerByNameAsync(matches[0].Server?.Name ?? "");
        }
        else
        {
            // Multiple matches, let user select
            AnsiConsole.MarkupLine($"[green]✓[/] Found [cyan]{matches.Count}[/] matches from multiple registries");
            AnsiConsole.WriteLine();

            var table = new Table()
                .Border(TableBorder.Rounded)
                .BorderColor(Color.Grey)
                .AddColumn(new TableColumn("[dim]#[/]").Centered())
                .AddColumn(new TableColumn("[cyan]Server Name[/]"))
                .AddColumn(new TableColumn("[yellow]Version[/]").Centered())
                .AddColumn(new TableColumn("[green]Registry[/]"))
                .AddColumn(new TableColumn("[dim]Description[/]"));

            for (int i = 0; i < matches.Count; i++)
            {
                var match = matches[i];
                var registryDisplay = match.RegistrySource ?? "Unknown";
                table.AddRow(
                    $"[dim]{i + 1}[/]",
                    $"[cyan]{match.Server?.Name?.EscapeMarkup()}[/]",
                    $"[yellow]{match.Server?.Version?.EscapeMarkup()}[/]",
                    $"[green]{registryDisplay.EscapeMarkup()}[/]",
                    $"[dim]{match.Server?.Description?.Substring(0, Math.Min(40, match.Server.Description?.Length ?? 0)).EscapeMarkup()}...[/]"
                );
            }

            AnsiConsole.Write(table);
            AnsiConsole.WriteLine();

            var selection = AnsiConsole.Prompt(
                new TextPrompt<int>($"[green]Select a server[/] [dim](1-{matches.Count})[/]:")
                    .ValidationErrorMessage("[red]Invalid selection[/]")
                    .Validate(n => n >= 1 && n <= matches.Count 
                        ? ValidationResult.Success() 
                        : ValidationResult.Error($"[red]Please select a number between 1 and {matches.Count}[/]"))
            );

            var selectedMatch = matches[selection - 1];
            selectedServer = await AnsiConsole.Status()
                .Spinner(Spinner.Known.Dots)
                .StartAsync($"Fetching [yellow]{selectedMatch.Server?.Name}[/]...", async ctx => 
                {
                    return await client.GetServerByNameAsync(selectedMatch.Server?.Name ?? "");
                });
        }
    }

    if (selectedServer == null)
    {
        AnsiConsole.MarkupLine($"[red]✗[/] Failed to retrieve server details");
        return 1;
    }

    // Validate and fix server data according to schema
    ValidateAndFixServerData(selectedServer);

    AnsiConsole.WriteLine();
    var panel = new Panel(
        new Markup($"[cyan]{selectedServer.Name?.EscapeMarkup()}[/] [yellow]v{selectedServer.Version?.EscapeMarkup()}[/]\n[dim]{selectedServer.Description?.EscapeMarkup()}[/]"))
        .Header("[green]✓ Selected Server[/]")
        .BorderColor(Color.Green)
        .Padding(1, 0);
    AnsiConsole.Write(panel);

    // Generate output filename if not provided
    if (string.IsNullOrEmpty(outputFile))
    {
        outputFile = $"{selectedServer.Name?.Replace("/", "-") ?? "server"}.json";
    }

    // Serialize with pretty print
    var options = new JsonSerializerOptions
    {
        WriteIndented = true,
        PropertyNamingPolicy = JsonNamingPolicy.CamelCase
    };

    var json = JsonSerializer.Serialize(selectedServer, options);
    await File.WriteAllTextAsync(outputFile, json);

    AnsiConsole.WriteLine();
    AnsiConsole.MarkupLine($"[green]✓[/] Saved to: [cyan]{outputFile}[/]");
    
    AnsiConsole.WriteLine();
    
    // Check if there were any errors
    var hasErrors = selectedServer.Name == null || 
                    selectedServer.Description == null || 
                    selectedServer.Version == null ||
                    (selectedServer.Packages?.Any(p => p.RegistryType == null || p.Identifier == null || p.Transport == null) ?? false);
    
    var nextSteps = new Table()
        .Border(TableBorder.None)
        .HideHeaders()
        .AddColumn("")
        .AddColumn("");
    
    if (hasErrors)
    {
        nextSteps
            .AddRow("[dim]1.[/]", $"[dim]Review and [red]fix errors[/] in:[/] [cyan]{outputFile}[/]")
            .AddRow("[dim]2.[/]", "[dim][red]Fix all validation errors[/] before publishing[/]")
            .AddRow("[dim]3.[/]", $"[dim]Move to:[/] [yellow]servers/pending/{Path.GetFileName(outputFile)}[/]")
            .AddRow("[dim]4.[/]", "[dim]Create PR for security team review[/]");
        
        var nextStepsPanel = new Panel(nextSteps)
            .Header("[red]⚠️  Action Required[/]")
            .BorderColor(Color.Red);
        
        AnsiConsole.Write(nextStepsPanel);
    }
    else
    {
        nextSteps
            .AddRow("[dim]1.[/]", $"[dim]Review the file:[/] [cyan]{outputFile}[/]")
            .AddRow("[dim]2.[/]", $"[dim]Move to:[/] [yellow]servers/pending/{Path.GetFileName(outputFile)}[/]")
            .AddRow("[dim]3.[/]", "[dim]Create PR for security team review[/]");
        
        var nextStepsPanel = new Panel(nextSteps)
            .Header("[yellow]📋 Next Steps[/]")
            .BorderColor(Color.Yellow);
        
        AnsiConsole.Write(nextStepsPanel);
    }

    return 0;
}
catch (Exception ex)
{
    AnsiConsole.WriteException(ex, ExceptionFormats.ShortenPaths | ExceptionFormats.ShortenTypes);
    return 1;
}

static void ValidateAndFixServerData(Server server)
{
    var warnings = new List<string>();
    var errors = new List<string>();

    // Validate name (required, must contain exactly one slash, length 3-200, pattern validation)
    if (string.IsNullOrEmpty(server.Name))
    {
        errors.Add("Name is required but missing");
    }
    else
    {
        if (server.Name.Length < 3 || server.Name.Length > 200)
        {
            errors.Add($"Name length {server.Name.Length} is outside valid range (3-200 chars)");
        }

        var slashCount = server.Name.Count(c => c == '/');
        if (slashCount != 1)
        {
            errors.Add($"Name '{server.Name}' should contain exactly one slash (found {slashCount})");
        }

        // Validate name pattern: ^[a-zA-Z0-9.-]+/[a-zA-Z0-9._-]+$
        if (!System.Text.RegularExpressions.Regex.IsMatch(server.Name, @"^[a-zA-Z0-9.-]+/[a-zA-Z0-9._-]+$"))
        {
            errors.Add($"Name '{server.Name}' doesn't match required pattern (reverse-DNS format)");
        }
    }

    // Validate description (required, length 1-100)
    if (string.IsNullOrEmpty(server.Description))
    {
        errors.Add("Description is required but missing");
    }
    else
    {
        if (server.Description.Length < 1)
        {
            errors.Add("Description cannot be empty");
        }
        else if (server.Description.Length > 100)
        {
            var originalLength = server.Description.Length;
            server.Description = server.Description.Substring(0, 97) + "...";
            warnings.Add($"Description auto-truncated from {originalLength} to 100 characters");
        }
    }

    // Validate version (required, max 255 chars, should not be "latest" or contain ranges)
    if (string.IsNullOrEmpty(server.Version))
    {
        errors.Add("Version is required but missing");
    }
    else
    {
        if (server.Version.Length > 255)
        {
            errors.Add($"Version length {server.Version.Length} exceeds maximum (255 chars)");
        }

        if (server.Version.Equals("latest", StringComparison.OrdinalIgnoreCase))
        {
            errors.Add("Version cannot be 'latest' - must be a specific version");
        }

        // Check for version ranges
        var rangePatterns = new[] { "^", "~", ">=", "<=", ">", "<", ".x", ".*" };
        if (rangePatterns.Any(p => server.Version.Contains(p)))
        {
            errors.Add($"Version '{server.Version}' appears to be a range - specific versions required");
        }
    }

    // Validate title (optional, but if present: 1-100 chars)
    if (!string.IsNullOrEmpty(server.Schema) && server.Schema.Length > 0)
    {
        // Validate title if it exists (Note: Server model doesn't have Title property, but schema defines it)
        // This would need to be added to the Server model if needed
    }

    // Validate websiteUrl (optional, but if present must be valid URI)
    if (!string.IsNullOrEmpty(server.WebsiteUrl))
    {
        if (!Uri.TryCreate(server.WebsiteUrl, UriKind.Absolute, out var websiteUri) || 
            (websiteUri.Scheme != "http" && websiteUri.Scheme != "https"))
        {
            errors.Add($"WebsiteUrl '{server.WebsiteUrl}' is not a valid HTTP/HTTPS URI");
        }
    }

    // Validate repository (if present, must have url and source)
    if (server.Repository != null)
    {
        if (string.IsNullOrEmpty(server.Repository.Url))
        {
            errors.Add("Repository.Url is required when repository is specified");
        }
        else if (!Uri.TryCreate(server.Repository.Url, UriKind.Absolute, out _))
        {
            errors.Add($"Repository.Url '{server.Repository.Url}' is not a valid URI");
        }

        if (string.IsNullOrEmpty(server.Repository.Source))
        {
            errors.Add("Repository.Source is required when repository is specified");
        }
    }

    // Validate packages
    if (server.Packages != null && server.Packages.Count > 0)
    {
        for (int i = 0; i < server.Packages.Count; i++)
        {
            var pkg = server.Packages[i];
            var prefix = $"Package[{i}]";

            // Required fields: registryType, identifier, transport
            if (string.IsNullOrEmpty(pkg.RegistryType))
            {
                errors.Add($"{prefix}.RegistryType is required");
            }

            if (string.IsNullOrEmpty(pkg.Identifier))
            {
                errors.Add($"{prefix}.Identifier is required");
            }

            if (pkg.Transport == null)
            {
                errors.Add($"{prefix}.Transport is required");
            }

            // Validate version if present (no "latest" or ranges)
            if (!string.IsNullOrEmpty(pkg.Version))
            {
                if (pkg.Version.Equals("latest", StringComparison.OrdinalIgnoreCase))
                {
                    errors.Add($"{prefix}.Version cannot be 'latest'");
                }

                var rangePatterns = new[] { "^", "~", ">=", "<=", ">", "<", ".x", ".*" };
                if (rangePatterns.Any(p => pkg.Version.Contains(p)))
                {
                    errors.Add($"{prefix}.Version '{pkg.Version}' appears to be a range");
                }
            }

            // Validate fileSha256 pattern if present: ^[a-f0-9]{64}$
            if (!string.IsNullOrEmpty(pkg.FileSha256))
            {
                if (!System.Text.RegularExpressions.Regex.IsMatch(pkg.FileSha256, @"^[a-f0-9]{64}$"))
                {
                    errors.Add($"{prefix}.FileSha256 must be 64 hex characters (SHA-256 hash)");
                }
            }

            // Validate registryBaseUrl if present
            if (!string.IsNullOrEmpty(pkg.RegistryBaseUrl))
            {
                if (!Uri.TryCreate(pkg.RegistryBaseUrl, UriKind.Absolute, out _))
                {
                    errors.Add($"{prefix}.RegistryBaseUrl '{pkg.RegistryBaseUrl}' is not a valid URI");
                }
            }

            // Validate transport
            if (pkg.Transport != null)
            {
                if (string.IsNullOrEmpty(pkg.Transport.Type))
                {
                    errors.Add($"{prefix}.Transport.Type is required");
                }

                // Validate transport URL for types that require it
                if (pkg.Transport.Type != "stdio" && string.IsNullOrEmpty(pkg.Transport.Url))
                {
                    errors.Add($"{prefix}.Transport.Url is required for '{pkg.Transport.Type}' transport");
                }
            }
        }
    }

    // Validate remotes
    if (server.Remotes != null && server.Remotes.Count > 0)
    {
        for (int i = 0; i < server.Remotes.Count; i++)
        {
            var remote = server.Remotes[i];
            var prefix = $"Remote[{i}]";

            if (string.IsNullOrEmpty(remote.Type))
            {
                errors.Add($"{prefix}.Type is required");
            }

            if (string.IsNullOrEmpty(remote.Url))
            {
                errors.Add($"{prefix}.Url is required");
            }
            else if (!Uri.TryCreate(remote.Url, UriKind.Absolute, out _))
            {
                errors.Add($"{prefix}.Url '{remote.Url}' is not a valid URI");
            }
        }
    }

    // Display warnings if any
    if (warnings.Count > 0)
    {
        AnsiConsole.WriteLine();
        AnsiConsole.MarkupLine("[yellow]⚠️  Warnings (auto-fixed):[/]");
        foreach (var warning in warnings)
        {
            AnsiConsole.MarkupLine($"  [dim]•[/] {warning.EscapeMarkup()}");
        }
    }

    // Display errors if any
    if (errors.Count > 0)
    {
        AnsiConsole.WriteLine();
        AnsiConsole.MarkupLine("[red]❌ Errors (must fix before publishing):[/]");
        foreach (var error in errors)
        {
            AnsiConsole.MarkupLine($"  [dim]•[/] {error.EscapeMarkup()}");
        }
    }
}