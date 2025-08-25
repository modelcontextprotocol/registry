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

var publishedIDRegex = regexp.MustCompile(`"x-io\.modelcontextprotocol\.registry":\s*\{[^}]*"id":\s*"([^"]+)"`)

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func getAnonymousToken() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registryURL+"/v0/auth/none", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get anonymous token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("auth endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResponse struct {
		RegistryToken string `json:"registry_token"`
		ExpiresAt     int    `json:"expires_at"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	if tokenResponse.RegistryToken == "" {
		return "", fmt.Errorf("received empty token from auth endpoint")
	}

	log.Printf("Got anonymous token (expires at %d)", tokenResponse.ExpiresAt)
	return tokenResponse.RegistryToken, nil
}

func run() error {
	examplesPath := filepath.Join("docs", "server-json", "examples.md")
	examples, err := examples(examplesPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Fatalf("%q not found; run this test from the repo root", examplesPath)
		}
		log.Fatalf("failed to extract examples from %q: %v", examplesPath, err)
	}

	log.Printf("Found %d examples in %q\n", len(examples), examplesPath)

	// Get anonymous token from the none endpoint
	token, err := getAnonymousToken()
	if err != nil {
		log.Fatalf("failed to get anonymous token: %v", err)
	}

	if err := os.WriteFile(".mcpregistry_registry_token", []byte(token), 0600); err != nil {
		log.Fatalf("failed to write token: %v", err)
	}
	defer os.Remove(".mcpregistry_registry_token")

	return publish(examples)
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
	cmd := exec.CommandContext(ctx, "./bin/publisher", "publish", "--mcp-file", filePath, "--registry-url", registryURL, "--auth-method", "none")
	cmd.WaitDelay = 100 * time.Millisecond

	out, err := cmd.CombinedOutput()
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return "", errors.New("publisher not found; did you run tests/integration/run.sh?")
	}
	output := strings.TrimSpace("publisher output:\n\t" + strings.ReplaceAll(string(out), "\n", "\n  \t"))
	if err != nil {
		return "", errors.New(output)
	}
	log.Println("  ✅", output)

	m := publishedIDRegex.FindStringSubmatch(output)
	if len(m) != 2 || m[1] == "" {
		return "", errors.New("didn't find ID in publisher output")
	}
	return m[1], nil
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

func examples(path string) ([]example, error) {
	b, err := os.ReadFile(path)
	if err != nil {
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
