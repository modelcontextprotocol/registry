package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	tokenFilePath = ".mcpregistry_token"

	// TODO: Replace this with the official owned OAuth client ID
	GithubClientID = "Ov23ct0x1531TPL3WJ9h"

	// GitHub OAuth URLs
	GitHubDeviceCodeURL  = "https://github.com/login/device/code"
	GitHubAccessTokenURL = "https://github.com/login/oauth/access_token"
)

// DeviceCodeResponse represents the response from GitHub's device code endpoint
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// AccessTokenResponse represents the response from GitHub's access token endpoint
type AccessTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Error       string `json:"error,omitempty"`
}

func main() {
	var registryURL string
	var mcpFilePath string
	var forceLogin bool
	var providedToken string

	flag.StringVar(&registryURL, "registry-url", "", "URL of the registry(required)")
	flag.StringVar(&mcpFilePath, "mcp-file", "", "path to the MCP file(required)")
	flag.BoolVar(&forceLogin, "login", false, "force a new login even if a token exists")
	flag.StringVar(&providedToken, "token", "", "use the provided token instead of GitHub authentication")

	flag.Parse()

	if registryURL == "" || mcpFilePath == "" {
		flag.Usage()
		return
	}

	var token string

	// If a token is provided via the command line, use it
	if providedToken != "" {
		token = providedToken
	} else {
		// Check if token exists or force login is requested
		_, statErr := os.Stat(tokenFilePath)
		if forceLogin || os.IsNotExist(statErr) {
			err := performDeviceFlowLogin()
			if err != nil {
				fmt.Printf("Failed to perform device flow login: %s\n", err.Error())
				return
			}
		}

		// Read the token from the file
		var err error
		token, err = readToken()
		if err != nil {
			fmt.Printf("Error reading token: %s\n", err.Error())
			return
		}
	}

	// Read MCP file
	mcpData, err := os.ReadFile(mcpFilePath)
	if err != nil {
		fmt.Printf("Error reading MCP file: %s\n", err.Error())
		return
	}

	// Publish to registry
	err = publishToRegistry(registryURL, mcpData, token)
	if err != nil {
		fmt.Printf("Failed to publish to registry: %s\n", err.Error())
		return
	}

	fmt.Println("Successfully published to registry!")
}

func performDeviceFlowLogin() error {
	// Device flow login logic using GitHub's device flow
	// First, request a device code
	deviceCode, userCode, verificationURI, err := requestDeviceCode()
	if err != nil {
		return fmt.Errorf("error requesting device code: %w", err)
	}

	// Display instructions to the user
	fmt.Println("\nTo authenticate, please:")
	fmt.Println("1. Go to:", verificationURI)
	fmt.Println("2. Enter code:", userCode)
	fmt.Println("3. Authorize this application")

	// Poll for the token
	fmt.Println("Waiting for authorization...")
	token, err := pollForToken(deviceCode)
	if err != nil {
		return fmt.Errorf("error polling for token: %w", err)
	}

	// Store the token locally
	err = saveToken(token)
	if err != nil {
		return fmt.Errorf("error saving token: %w", err)
	}

	fmt.Println("Successfully authenticated!")
	return nil
}

// requestDeviceCode initiates the device authorization flow
func requestDeviceCode() (string, string, string, error) {
	payload := map[string]string{
		"client_id": GithubClientID,
		"scope":     "read:org read:user",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", "", "", err
	}

	req, err := http.NewRequest("POST", GitHubDeviceCodeURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("request device code failed: %s", body)
	}

	var deviceCodeResp DeviceCodeResponse
	err = json.Unmarshal(body, &deviceCodeResp)
	if err != nil {
		return "", "", "", err
	}

	return deviceCodeResp.DeviceCode, deviceCodeResp.UserCode, deviceCodeResp.VerificationURI, nil
}

// pollForToken polls for access token after user completes authorization
func pollForToken(deviceCode string) (string, error) {
	payload := map[string]string{
		"client_id":   GithubClientID,
		"device_code": deviceCode,
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	// Default polling interval and expiration time
	interval := 5    // seconds
	expiresIn := 900 // 15 minutes
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)

	for time.Now().Before(deadline) {
		req, err := http.NewRequest("POST", GitHubAccessTokenURL, bytes.NewBuffer(jsonData))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", err
		}

		var tokenResp AccessTokenResponse
		err = json.Unmarshal(body, &tokenResp)
		if err != nil {
			return "", err
		}

		if tokenResp.Error == "authorization_pending" {
			// User hasn't authorized yet, wait and retry
			fmt.Print(".")
			time.Sleep(time.Duration(interval) * time.Second)
			continue
		}

		if tokenResp.Error != "" {
			return "", fmt.Errorf("token request failed: %s", tokenResp.Error)
		}

		if tokenResp.AccessToken != "" {
			fmt.Println() // Add newline after dots
			return tokenResp.AccessToken, nil
		}

		// If we reach here, something unexpected happened
		return "", fmt.Errorf("failed to obtain access token")
	}

	return "", fmt.Errorf("device code authorization timed out")
}

// saveToken saves the GitHub access token to a local file
func saveToken(token string) error {
	return os.WriteFile(tokenFilePath, []byte(token), 0600)
}

// readToken reads the GitHub access token from a local file
func readToken() (string, error) {
	tokenData, err := os.ReadFile(tokenFilePath)
	if err != nil {
		return "", err
	}
	return string(tokenData), nil
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
	publishReq := map[string]interface{}{
		"server_detail": mcpDetails,
	}

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
	req, err := http.NewRequest("POST", publishURL, bytes.NewBuffer(jsonData))
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

	println(string(body))
	return nil
}
