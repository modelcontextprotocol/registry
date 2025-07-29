// Package v0 contains API handlers for version 0 of the API
package v0

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"

	"github.com/modelcontextprotocol/registry/internal/appinfo"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
)

const (
	DBStatusConnected    = "connected"
	DBStatusDisconnected = "disconnected"
)

type DatabaseType string
type HealthResponse struct {
	Status         string          `json:"status"`
	GitHubClientID string          `json:"github_client_id"`
	Database       *DatabaseHealth `json:"database"`
	Uptime         string          `json:"uptime"`
	Version        string          `json:"version"`
	Memory         *MemoryStats    `json:"memory"`
}

type DatabaseHealth struct {
	Status          string       `json:"status"`
	Type            DatabaseType `json:"type"`
	CollectionCount int          `json:"collection_count"`
}

type MemoryStats struct {
	Alloc string `json:"alloc"`
	Sys   string `json:"sys"`
}

// HealthHandler returns a handler for health check endpoint
func HealthHandler(cfg *config.Config, db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connInfo := db.Connection()
		dbStatus := DBStatusDisconnected
		if connInfo.IsConnected {
			dbStatus = DBStatusConnected
		}
		databaseHealth := DatabaseHealth{
			Status:          dbStatus,
			Type:            DatabaseType(connInfo.Type),
			CollectionCount: connInfo.CollectionCount,
		}

		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		if err := json.NewEncoder(w).Encode(HealthResponse{
			Status:         "ok",
			GitHubClientID: cfg.GithubClientID,
			Database:       &databaseHealth,
			Uptime:         appinfo.GetUptimeString(),
			Version:        cfg.Version,
			Memory: &MemoryStats{
				Alloc: fmt.Sprintf("%.1f MB", float64(m.Alloc)/1024/1024),
				Sys:   fmt.Sprintf("%.1f MB", float64(m.Sys)/1024/1024),
			},
		}); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		}
	}
}
