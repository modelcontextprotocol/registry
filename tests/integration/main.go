package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

const registryURL = "http://localhost:8080"

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	examplesPath := filepath.Join("docs", "reference", "server-json", "generic-server-json.md")
	examples, err := getExamples(examplesPath)
	if err != nil {
		log.Fatalf("failed to extract examples: %v", err)
	}
	log.Printf("Found %d examples in %q\n", len(examples), examplesPath)

	// Set up authentication using the new login workflow
	err = setupPublisherAuth()
	if err != nil {
		log.Fatalf("failed to set up publisher auth: %v", err)
	}
	defer cleanupPublisherAuth()

	return publish(examples)
}

func setupPublisherAuth() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "./bin/publisher", "login", "none", "--registry", registryURL)
	cmd.WaitDelay = 100 * time.Millisecond

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("login failed: %s", string(out))
	}

	log.Printf("Publisher login successful: %s", strings.TrimSpace(string(out)))
	return nil
}

func cleanupPublisherAuth() {
	// Clean up the token file
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	tokenPath := filepath.Join(homeDir, ".mcp_publisher_token")
	os.Remove(tokenPath)
}

func publish(examples []example) error {
	published := 0
	for _, example := range examples {
		if err := publishExample(example); err != nil {
			log.Printf("  ⛔ %v", err)
			continue
		}
		published++
	}

	msg := fmt.Sprintf("published %d/%d examples", published, len(examples))
	if published != len(examples) {
		return errors.New(msg)
	}
	log.Println(msg)
	return nil
}

func publishExample(example example) error {
	log.Printf("Publishing example starting on line %d", example.line)

	expected, err := parseExample(example)
	if err != nil {
		return err
	}

	if err := publishToRegistry(expected, example.line); err != nil {
		return err
	}

	log.Print("  ✅ registry response matches example\n\n")
	return nil
}

func parseExample(example example) (*apiv0.ServerJSON, error) {
	expected := &apiv0.ServerJSON{}
	if err := json.Unmarshal(example.content, expected); err != nil {
		return nil, fmt.Errorf("example isn't valid JSON: %w", err)
	}

	// Remove any existing namespace prefix and add anonymous prefix
	if !strings.HasPrefix(expected.Name, "io.modelcontextprotocol.anonymous/") {
		parts := strings.SplitN(expected.Name, "/", 2)
		serverName := parts[len(parts)-1]
		expected.Name = "io.modelcontextprotocol.anonymous/" + serverName
	}

	return expected, nil
}

func publishToRegistry(expected *apiv0.ServerJSON, line int) error {
	content, _ := json.Marshal(expected)
	p := filepath.Join("bin", fmt.Sprintf("example-line-%d.json", line))
	if err := os.WriteFile(p, content, 0600); err != nil {
		return fmt.Errorf("failed to write example JSON to %s: %w", p, err)
	}
	defer os.Remove(p)

	id, err := runPublisher(p)
	if err != nil {
		log.Printf("  ⛔ Failed to get server ID: %v", err)
		return err
	}
	log.Printf("  📋 Got server ID: %s", id)

	return verifyPublishedServer(id, expected)
}

func runPublisher(filePath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "./bin/publisher", "publish", filePath)
	cmd.WaitDelay = 100 * time.Millisecond

	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace("publisher output:\n\t" + strings.ReplaceAll(string(out), "\n", "\n  \t"))
	if err != nil {
		return "", errors.New(output)
	}
	log.Println("  ✅", output)

	// Get the server name from the file to look up the ID
	serverName, err := getServerNameFromFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to get server name from file: %w", err)
	}

	// Add a small delay to ensure database consistency
	time.Sleep(100 * time.Millisecond)

	// Find the server in the registry by name
	return findServerIDByName(serverName)
}

func getServerNameFromFile(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	var serverData map[string]any
	if err := json.Unmarshal(data, &serverData); err != nil {
		return "", err
	}

	// Handle both old ServerDetail format and new PublishRequest format
	if server, exists := serverData["server"]; exists {
		// New PublishRequest format
		if serverMap, ok := server.(map[string]any); ok {
			if name, ok := serverMap["name"].(string); ok {
				return name, nil
			}
		}
	} else if name, ok := serverData["name"].(string); ok {
		// Old ServerDetail format
		return name, nil
	}

	return "", errors.New("could not find server name in file")
}

func findServerIDByName(serverName string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registryURL+"/v0/servers", nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("registry responded %d: %s", resp.StatusCode, string(body))
	}

	var serverList *apiv0.ServerListResponse
	if err := json.NewDecoder(resp.Body).Decode(&serverList); err != nil {
		return "", fmt.Errorf("failed to decode server list: %w", err)
	}

	// Find the server with matching name
	var foundServers []string
	for _, server := range serverList.Servers {
		if server.Name == serverName {
			foundServers = append(foundServers, fmt.Sprintf("ServerID:%s VersionID:%s IsLatest:%t", server.Meta.Official.ServerID, server.Meta.Official.VersionID, server.Meta.Official.IsLatest))
			if server.Meta.Official.IsLatest {
				return server.Meta.Official.ServerID, nil
			}
		}
	}

	if len(foundServers) > 0 {
		return "", fmt.Errorf("found server %s but none marked as latest: %v", serverName, foundServers)
	}
	return "", fmt.Errorf("could not find any server with name %s", serverName)
}

func verifyPublishedServer(id string, expected *apiv0.ServerJSON) error {
	log.Printf("  🔍 Verifying server with ID: %s", id)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registryURL+"/v0/servers/"+id, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	content, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read registry response: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("registry responded %d: %s", res.StatusCode, string(content))
	}

	var actual *apiv0.ServerJSON
	if err := json.Unmarshal(content, &actual); err != nil {
		return fmt.Errorf("failed to unmarshal registry response: %w", err)
	}

	if err := compareServerJSON(expected, actual); err != nil {
		return fmt.Errorf(`example "%s": %w`, expected.Name, err)
	}
	return nil
}

type example struct {
	content []byte
	line    int
}

func getExamples(path string) ([]example, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("examples not found; run this test from the repo root")
		return nil, err
	}

	// matches JSON code blocks in markdown,
	// captures everything between ```json and ```
	re := regexp.MustCompile("(?s)```json\n(.*?)\n```")
	matches := re.FindAllSubmatchIndex(b, -1)

	examples := make([]example, len(matches))
	for i, match := range matches {
		if len(match) < 4 {
			// should never happen
			return nil, fmt.Errorf("invalid match: expected 4 indices but got %d", len(match))
		}
		start, end := match[2], match[3]
		// line numbers start at 1
		line := 1 + bytes.Count(b[:start], []byte{'\n'})
		examples[i] = example{
			content: b[start:end],
			line:    line,
		}
	}

	return examples, nil
}

func compareServerJSON(expected, actual *apiv0.ServerJSON) error {
	// Compare core fields (ignore Meta as it contains registry-generated data)
	if expected.Name != actual.Name {
		return fmt.Errorf("name mismatch: expected %q, got %q", expected.Name, actual.Name)
	}
	if expected.Description != actual.Description {
		return fmt.Errorf("description mismatch: expected %q, got %q", expected.Description, actual.Description)
	}
	if expected.Status != actual.Status {
		return fmt.Errorf("status mismatch: expected %q, got %q", expected.Status, actual.Status)
	}
	if expected.Version != actual.Version {
		return fmt.Errorf("version mismatch: expected %+v, got %+v", expected.Version, actual.Version)
	}

	// Compare repository
	if !reflect.DeepEqual(expected.Repository, actual.Repository) {
		return fmt.Errorf("repository mismatch: expected %+v, got %+v", expected.Repository, actual.Repository)
	}

	// Compare packages
	if !reflect.DeepEqual(expected.Packages, actual.Packages) {
		return fmt.Errorf("packages mismatch: expected %+v, got %+v", expected.Packages, actual.Packages)
	}

	// Compare remotes
	if !reflect.DeepEqual(expected.Remotes, actual.Remotes) {
		return fmt.Errorf("remotes mismatch: expected %+v, got %+v", expected.Remotes, actual.Remotes)
	}

	// Compare only publisher metadata if present
	if expected.Meta != nil && expected.Meta.PublisherProvided != nil {
		if actual.Meta == nil || actual.Meta.PublisherProvided == nil {
			return fmt.Errorf("expected publisher metadata, but got none")
		}
		if !reflect.DeepEqual(expected.Meta.PublisherProvided, actual.Meta.PublisherProvided) {
			return fmt.Errorf("publisher metadata mismatch: expected %+v, got %+v", expected.Meta.PublisherProvided, actual.Meta.PublisherProvided)
		}
	}

	return nil
}
