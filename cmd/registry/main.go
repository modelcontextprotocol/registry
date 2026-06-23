package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Version info for the MCP Registry application
// These variables are injected at build time via ldflags
var (
	// Version is the current version of the MCP Registry application
	Version = "dev"

	// BuildTime is the time at which the binary was built
	BuildTime = "unknown"

	// GitCommit is the git commit that was compiled
	GitCommit = "unknown"
)

// ServerJSON represents a single MCP server definition from seed.json
type ServerJSON struct {
	Schema      string                 `json:"$schema,omitempty"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Repository  map[string]interface{} `json:"repository,omitempty"`
	Version     string                 `json:"version"`
	Icons       []map[string]interface{} `json:"icons,omitempty"`
	Packages    []map[string]interface{} `json:"packages,omitempty"`
}

// InMemoryRegistry holds all servers loaded from seed.json
type InMemoryRegistry struct {
	servers map[string][]*ServerJSON // map[serverName][]versions
}

// Global registry instance
var registry *InMemoryRegistry

// Global allowlist (comma-separated server names)
var allowlist []string

func main() {
	// Parse command line flags
	log.Printf("Starting MCP Registry Application v%s (commit: %s) [Lightweight Mode - No Database]", Version, GitCommit)

	// Get port from environment variable (default from docker-compose.yml expects 8080)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Get seed file path from environment variable
	seedPath := os.Getenv("MCP_REGISTRY_SEED_FROM")
	if seedPath == "" {
		seedPath = "/data/seed.json"
	}

	// Get allowlist from environment variable
	allowlistEnv := os.Getenv("MCP_REGISTRY_ALLOWED_SERVERS")
	if allowlistEnv != "" {
		// Parse comma-separated list
		for _, name := range strings.Split(allowlistEnv, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				allowlist = append(allowlist, name)
			}
		}
		log.Printf("🔒 Allowlist enabled: %v", allowlist)
	} else {
		log.Printf("ℹ️  No allowlist configured - all servers visible")
	}

	// Load seed data
	log.Printf("Loading seed data from: %s", seedPath)
	var err error
	registry, err = loadSeedData(seedPath)
	if err != nil {
		log.Printf("Failed to load seed data: %v", err)
		log.Printf("Continuing with empty registry...")
		registry = &InMemoryRegistry{servers: make(map[string][]*ServerJSON)}
	} else {
		log.Printf("✅ Loaded %d servers successfully", len(registry.servers))
	}

	// Setup HTTP routes
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/healthz", handleHealthz)

	// API v0.1 endpoints
	mux.HandleFunc("/v0.1/ping", handlePing)
	mux.HandleFunc("/v0.1/servers", handleServers)

	// Catch-all for server-specific routes
	mux.HandleFunc("/v0.1/servers/", handleServerVersions)

	// Wrap with CORS and logging middleware
	handler := corsMiddleware(loggingMiddleware(mux))

	// Create HTTP server
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	// 1. 在啟動前，就大聲通知我們和 Azure 它是活著的！
    log.Printf("🚀 MCP Registry Web Server 正在初始化...")
    log.Printf("📡 準備監聽連接埠: %s", port)
    log.Printf("🔗 預期健全檢查路徑: http://localhost:%s/healthz", port)

    serverErrors := make(chan error, 1)

    // 在背景啟動監聽，並把結果丟進通道
    go func() {
        // ListenAndServe 會一直卡在背景，直到發生錯誤
        serverErrors <- server.ListenAndServe()
    }()

    // 2. 建立關機訊號通道
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

    log.Printf("🟢 系統已進入守護模式，正在等待請求...")

    // 3. 核心監聽
    select {
    case err := <-serverErrors:
        log.Fatalf("❌ Web Server 啟動失敗: %v", err)

    case sig := <-quit:
        log.Printf("👋 收到關機訊號 [%v]，開始優雅關閉...", sig)

        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()

        if err := server.Shutdown(ctx); err != nil {
            log.Printf("❌ 迫使伺服器關閉時發生錯誤: %v", err)
        }
    }

    log.Println("🎉 MCP Registry 安全退出")
}

// loadSeedData reads and parses the seed.json file
func loadSeedData(path string) (*InMemoryRegistry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read seed file: %w", err)
	}

	var serverList []*ServerJSON
	if err := json.Unmarshal(data, &serverList); err != nil {
		return nil, fmt.Errorf("failed to parse seed file: %w", err)
	}

	reg := &InMemoryRegistry{
		servers: make(map[string][]*ServerJSON),
	}

	// Group servers by name
	for _, server := range serverList {
		reg.servers[server.Name] = append(reg.servers[server.Name], server)
	}

	return reg, nil
}

// isServerAllowed checks if a server name is in the allowlist
func isServerAllowed(serverName string) bool {
	// If no allowlist configured, allow all
	if len(allowlist) == 0 {
		return true
	}

	// Check if server name matches any allowlist pattern
	for _, pattern := range allowlist {
		// Support wildcard patterns like "io.figma/*"
		if strings.HasSuffix(pattern, "/*") {
			prefix := strings.TrimSuffix(pattern, "/*")
			if strings.HasPrefix(serverName, prefix+"/") {
				return true
			}
		} else if pattern == serverName {
			return true
		}
	}

	return false
}

// Middleware: CORS
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Middleware: Logging
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("→ %s %s %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
		log.Printf("← %s %s completed in %v", r.Method, r.URL.Path, time.Since(start))
	})
}

// Handler: Health check
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
	})
}

// Handler: Ping
func handlePing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "pong",
	})
}

// Handler: List servers / Create server
func handleServers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleListServers(w, r)
	case http.MethodPost:
		handleCreateServer(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// Handler: List all servers
func handleListServers(w http.ResponseWriter, r *http.Request) {
	// Build response with all latest versions
	var serverList []map[string]interface{}
	
	for serverName, versions := range registry.servers {
		// Check allowlist
		if !isServerAllowed(serverName) {
			continue
		}
		
		if len(versions) > 0 {
			// Get the latest version (last in array)
			latest := versions[len(versions)-1]
			
			// Build the server object with all fields
			serverObj := map[string]interface{}{
				"name":        latest.Name,
				"description": latest.Description,
				"version":     latest.Version,
			}
			
			// Add optional fields
			if latest.Schema != "" {
				serverObj["$schema"] = latest.Schema
			}
			if latest.Repository != nil {
				serverObj["repository"] = latest.Repository
			}
			if latest.Icons != nil {
				serverObj["icons"] = latest.Icons
			}
			if latest.Packages != nil {
				serverObj["packages"] = latest.Packages
			}
			
			// Build the _meta object
			metaObj := map[string]interface{}{
				"io.modelcontextprotocol.registry/official": map[string]interface{}{
					"status":          "active",
					"statusChangedAt": time.Now().Format(time.RFC3339),
					"publishedAt":     time.Now().Format(time.RFC3339),
					"updatedAt":       time.Now().Format(time.RFC3339),
					"isLatest":        true,
				},
			}
			
			// Combine server and _meta into the response format
			serverEntry := map[string]interface{}{
				"server": serverObj,
				"_meta":  metaObj,
			}
			
			serverList = append(serverList, serverEntry)
		}
	}

	response := map[string]interface{}{
		"servers": serverList,
		"metadata": map[string]interface{}{
			"count": len(serverList),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// Handler: Create server (POST)
func handleCreateServer(w http.ResponseWriter, r *http.Request) {
	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	log.Printf("📥 POST /v0.1/servers - Received body (%d bytes):", len(body))
	log.Printf("%s", string(body))

	// Parse the JSON to validate it
	var serverData map[string]interface{}
	if err := json.Unmarshal(body, &serverData); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Return 201 Created with the received data
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Server created (mock mode - not persisted)",
		"data":    serverData,
	})
}

// Handler: Get server by name and version
func handleServerVersions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse URL path: /v0.1/servers/{name}/versions/{version}
	path := strings.TrimPrefix(r.URL.Path, "/v0.1/servers/")
	parts := strings.Split(path, "/versions/")
	
	if len(parts) != 2 {
		http.Error(w, "Invalid URL format. Expected: /v0.1/servers/{name}/versions/{version}", http.StatusBadRequest)
		return
	}

	// URL decode the server name
	serverName, err := url.QueryUnescape(parts[0])
	if err != nil {
		http.Error(w, "Invalid server name encoding", http.StatusBadRequest)
		return
	}

	version := parts[1]

	// Check allowlist
	if !isServerAllowed(serverName) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "Server not found",
			"name":    serverName,
			"version": version,
		})
		return
	}

	// Get server from registry
	versions, exists := registry.servers[serverName]
	if !exists || len(versions) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "Server not found",
			"name":    serverName,
			"version": version,
		})
		return
	}

	var targetServer *ServerJSON
	if version == "latest" {
		// Return the last version (latest)
		targetServer = versions[len(versions)-1]
	} else {
		// Find specific version
		for _, v := range versions {
			if v.Version == version {
				targetServer = v
				break
			}
		}
	}

	if targetServer == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "Version not found",
			"name":    serverName,
			"version": version,
		})
		return
	}

	// Build the server object with all fields
	serverObj := map[string]interface{}{
		"name":        targetServer.Name,
		"description": targetServer.Description,
		"version":     targetServer.Version,
	}

	// Add optional fields
	if targetServer.Schema != "" {
		serverObj["$schema"] = targetServer.Schema
	}
	if targetServer.Repository != nil {
		serverObj["repository"] = targetServer.Repository
	}
	if targetServer.Icons != nil {
		serverObj["icons"] = targetServer.Icons
	}
	if targetServer.Packages != nil {
		serverObj["packages"] = targetServer.Packages
	}

	// Build the _meta object
	metaObj := map[string]interface{}{
		"io.modelcontextprotocol.registry/official": map[string]interface{}{
			"status":          "active",
			"statusChangedAt": time.Now().Format(time.RFC3339),
			"publishedAt":     time.Now().Format(time.RFC3339),
			"updatedAt":       time.Now().Format(time.RFC3339),
			"isLatest":        version == "latest" || targetServer.Version == version,
		},
	}

	// Build response with server and _meta structure
	response := map[string]interface{}{
		"server": serverObj,
		"_meta":  metaObj,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
