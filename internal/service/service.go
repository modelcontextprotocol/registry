package service

import (
	"context"

	"github.com/modelcontextprotocol/registry/internal/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// StatusChangeRequest represents a request to change a server's status
type StatusChangeRequest struct {
	NewStatus     model.Status `json:"newStatus"`
	StatusMessage *string      `json:"statusMessage,omitempty"`
}

// RegistryService defines the interface for registry operations
type RegistryService interface {
	// ListServers retrieve all servers with optional filtering
	ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	// GetServerByName retrieve latest version of a server by server name
	GetServerByName(ctx context.Context, serverName string, includeDeleted bool) (*apiv0.ServerResponse, error)
	// GetServerByNameAndVersion retrieve specific version of a server by server name and version
	GetServerByNameAndVersion(ctx context.Context, serverName string, version string, includeDeleted bool) (*apiv0.ServerResponse, error)
	// GetAllVersionsByServerName retrieve all versions of a server by server name
	GetAllVersionsByServerName(ctx context.Context, serverName string, includeDeleted bool) ([]*apiv0.ServerResponse, error)
	// CreateServer creates a new server version
	CreateServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	// UpdateServer updates an existing server and optionally its status
	UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, statusChange *StatusChangeRequest) (*apiv0.ServerResponse, error)
	// UpdateServerStatus updates only the status metadata of a server version
	UpdateServerStatus(ctx context.Context, serverName, version string, statusChange *StatusChangeRequest) (*apiv0.ServerResponse, error)
	// UpdateAllVersionsStatus updates the status metadata of all versions of a server in a single transaction
	UpdateAllVersionsStatus(ctx context.Context, serverName string, statusChange *StatusChangeRequest) ([]*apiv0.ServerResponse, error)
}
