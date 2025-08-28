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
)

const registryURL = "http://localhost:8080"

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	examplesPath := filepath.Join("docs", "server-json", "examples.md")
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

func parseExample(example example) (map[string]any, error) {
	expected := map[string]any{}
	if err := json.Unmarshal(example.content, &expected); err != nil {
		return nil, fmt.Errorf("example isn't valid JSON: %w", err)
	}

	// Handle both old ServerDetail format and new PublishRequest format
	var serverData map[string]any
	if server, exists := expected["server"]; exists {
		// New PublishRequest format
		serverData = server.(map[string]any)
	} else {
		// Old ServerDetail format (backward compatibility)
		serverData = expected
	}

	// Remove any existing namespace prefix and add anonymous prefix
	if !strings.HasPrefix(serverData["name"].(string), "io.modelcontextprotocol.anonymous/") {
		parts := strings.SplitN(serverData["name"].(string), "/", 2)
		serverName := parts[len(parts)-1]
		serverData["name"] = "io.modelcontextprotocol.anonymous/" + serverName
	}

	// Update the expected structure if it's PublishRequest format
	if _, exists := expected["server"]; exists {
		expected["server"] = serverData
	}

	return expected, nil
}

func publishToRegistry(expected map[string]any, line int) error {
	content, _ := json.Marshal(expected)
	p := filepath.Join("bin", fmt.Sprintf("example-line-%d.json", line))
	if err := os.WriteFile(p, content, 0600); err != nil {
		return fmt.Errorf("failed to write example JSON to %s: %w", p, err)
	}
	defer os.Remove(p)

	id, err := runPublisher(p)
	if err != nil {
		return err
	}

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

	var serverList struct {
		Servers []map[string]any `json:"servers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&serverList); err != nil {
		return "", fmt.Errorf("failed to decode server list: %w", err)
	}

	// Find the server with matching name
	for _, server := range serverList.Servers {
		if registryMeta, ok := server["x-io.modelcontextprotocol.registry"].(map[string]any); ok {
			if id, ok := registryMeta["id"].(string); ok {
				if serverData, ok := server["server"].(map[string]any); ok {
					if name, ok := serverData["name"].(string); ok && name == serverName {
						return id, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("could not find server with name %s", serverName)
}

func verifyPublishedServer(id string, expected map[string]any) error {
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

	actual := map[string]any{}
	if err := json.Unmarshal(content, &actual); err != nil {
		return fmt.Errorf("failed to unmarshal registry response: %w", err)
	}

	// Both API response and expected are now in extension wrapper format
	// Compare the server portions of both
	actualServer, ok := actual["server"]
	if !ok {
		return fmt.Errorf("expected server field in registry response")
	}

	// Extract expected server portion for comparison
	expectedServer := expected
	if server, exists := expected["server"]; exists {
		expectedServer = server.(map[string]any)
	}

	if err := compare(expectedServer, actualServer); err != nil {
		return fmt.Errorf(`example "%s": %w`, expectedServer["name"], err)
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

// compare performs a deep comparison of JSON values. It returns an error when an expected value
// isn't in actual, unless the expected value is the zero value for its type. This exception
// is necessary because registry model fields are typically tagged "omitempty". A field having a
// zero value may therefore not be present in a registry response. compare doesn't consider whether
// actual has additional fields not in expected; it only checks that all expected fields are present.
func compare(expected, actual any) error {
	if reflect.ValueOf(expected).IsZero() {
		return nil
	}
	if actual == nil {
		return fmt.Errorf("expected %v, got nil", expected)
	}

	switch expectedValue := expected.(type) {
	case map[string]any:
		actualMap, ok := actual.(map[string]any)
		if !ok {
			return fmt.Errorf("expected map, got %T", actual)
		}
		for k, v := range expectedValue {
			// note key may not be present in actualMap, if the value would be zero
			if actualValue, ok := actualMap[k]; ok {
				if err := compare(v, actualValue); err != nil {
					return fmt.Errorf("key %q: %w", k, err)
				}
			}
		}
		return nil
	case []any:
		actualSlice, ok := actual.([]any)
		if !ok {
			return fmt.Errorf("expected array, got %T", actual)
		}
		for _, expectedItem := range expectedValue {
			found := false
			for _, actualItem := range actualSlice {
				if err := compare(expectedItem, actualItem); err == nil {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("%v missing in actual array", expectedItem)
			}
		}
		return nil
	default:
		if expected != actual {
			return fmt.Errorf("expected %v, got %v", expected, actual)
		}
		return nil
	}
}
