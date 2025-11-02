// This tool was created by Claude Code as a simple way to kick the tires on data migrations
// by fetching production data from the public registry API.
// It is not intended for production use.
//
//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type ServerResponse struct {
	Servers  []json.RawMessage `json:"servers"`
	Metadata struct {
		NextCursor string `json:"next_cursor,omitempty"`
		Count      int    `json:"count"`
	} `json:"metadata"`
}

func main() {
	baseURL := "https://registry.modelcontextprotocol.io/v0/servers"
	var allServers []json.RawMessage
	cursor := ""
	pageCount := 0

	for {
		pageCount++
		url := baseURL
		if cursor != "" {
			url = fmt.Sprintf("%s?cursor=%s", baseURL, cursor)
		}

		fmt.Printf("Fetching page %d: %s\n", pageCount, url)

		resp, err := http.Get(url)
		if err != nil {
			panic(fmt.Sprintf("Failed to fetch: %v", err))
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			panic(fmt.Sprintf("Failed to read body: %v", err))
		}

		var serverResp ServerResponse
		if err := json.Unmarshal(body, &serverResp); err != nil {
			panic(fmt.Sprintf("Failed to parse JSON: %v", err))
		}

		allServers = append(allServers, serverResp.Servers...)
		fmt.Printf("  Got %d servers (total: %d)\n", len(serverResp.Servers), len(allServers))

		if serverResp.Metadata.NextCursor == "" {
			break
		}
		cursor = serverResp.Metadata.NextCursor

		// Be nice to the API
		time.Sleep(100 * time.Millisecond)
	}

	// Save all servers to a file
	output := map[string]interface{}{
		"servers": allServers,
		"count":   len(allServers),
		"fetched": time.Now().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		panic(fmt.Sprintf("Failed to marshal output: %v", err))
	}

	outputFile := "production_servers.json"
	if err := os.WriteFile(outputFile, data, 0644); err != nil {
		panic(fmt.Sprintf("Failed to write file: %v", err))
	}

	fmt.Printf("\nDone! Saved %d servers to %s\n", len(allServers), outputFile)
}
