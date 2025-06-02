package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/registry/tools/publisher/auth"
	"github.com/modelcontextprotocol/registry/tools/publisher/auth/github"
)

func main() {
	var registryURL string
	var mcpFilePath string
	var forceLogin bool
	var authMethod string

	// Command-line flags for configuration
	flag.StringVar(&registryURL, "registry-url", "", "URL of the registry (required)")
	flag.StringVar(&mcpFilePath, "mcp-file", "", "path to the MCP file (required)")
	flag.BoolVar(&forceLogin, "login", false, "force a new login even if a token exists")
	flag.StringVar(&authMethod, "auth-method", "github-oauth", "authentication method to use (default: github-oauth)")

	flag.Parse()

	if registryURL == "" || mcpFilePath == "" {
		flag.Usage()
		return
	}

	// Read MCP file
	mcpData, err := os.ReadFile(mcpFilePath)
	if err != nil {
		log.Printf("Error reading MCP file: %s\n", err.Error())
		return
	}

	var authProvider auth.Provider // Determine the authentication method
	switch authMethod {
	case "github-oauth":
		log.Println("Using GitHub OAuth for authentication")
		authProvider = github.NewOAuthProvider(forceLogin, registryURL)
	default:
		log.Printf("Unsupported authentication method: %s\n", authMethod)
		return
	}

	// Check if login is needed and perform authentication
	ctx := context.Background()
	if authProvider.NeedsLogin() {
		err := authProvider.Login(ctx)
		if err != nil {
			log.Printf("Failed to authenticate with %s: %s\n", authProvider.Name(), err.Error())
			return
		}
	}

	// Get the token
	token, err := authProvider.GetToken(ctx)
	if err != nil {
		log.Printf("Error getting token from %s: %s\n", authProvider.Name(), err.Error())
		return
	}

	// Publish to registry
	err = publishToRegistry(registryURL, mcpData, token)
	if err != nil {
		log.Printf("Failed to publish to registry: %s\n", err.Error())
		return
	}

	log.Println("Successfully published to registry!")
}

// publishToRegistry sends the MCP server details to the registry with authentication
func publishToRegistry(registryURL string, mcpData []byte, token string) error {
	// Parse the MCP JSON data
	var mcpDetails map[string]interface{}
	err := json.Unmarshal(mcpData, &mcpDetails)
	if err != nil {
		return fmt.Errorf("error parsing mcp.json file: %w", err)
	}

	// Create the publish request payload (without authentication)
	publishReq := mcpDetails

	// Convert the request to JSON
	jsonData, err := json.Marshal(publishReq)
	if err != nil {
		return fmt.Errorf("error serializing request: %w", err)
	}

	// Ensure the URL ends with the publish endpoint
	if !strings.HasSuffix(registryURL, "/") {
		registryURL += "/"
	}
	publishURL := registryURL + "v0/publish"

	// Create and send the request
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, publishURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	// Read and check the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("publication failed with status %d: %s", resp.StatusCode, body)
	}

	log.Println(string(body))
	return nil
}
