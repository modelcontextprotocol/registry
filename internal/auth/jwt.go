package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/model"
)

// PermissionAction represents the type of action that can be performed
type PermissionAction string

const (
	PermissionActionPublish PermissionAction = "publish"
	// Intended for admins taking moderation actions only, at least for now
	PermissionActionEdit PermissionAction = "edit"
)

type Permission struct {
	Action          PermissionAction `json:"action"`   // The action type (publish or edit)
	ResourcePattern string           `json:"resource"` // e.g., "io.github.username/*"
}

// JWTClaims represents the claims for the Registry JWT token
type JWTClaims struct {
	jwt.RegisteredClaims
	// Authentication method used to obtain this token
	AuthMethod        model.AuthMethod `json:"auth_method"`
	AuthMethodSubject string           `json:"auth_method_sub"`
	Permissions       []Permission     `json:"permissions"`
}

type TokenResponse struct {
	RegistryToken string `json:"registry_token"`
	ExpiresAt     int    `json:"expires_at"`
}

// JWTManager handles JWT token operations
type JWTManager struct {
	secretKey     []byte
	tokenDuration time.Duration
}

func NewJWTManager(cfg *config.Config) *JWTManager {
	// Use a configurable secret key, fallback to a default for development
	// In production, this should come from configuration
	secretKey := []byte("registry-jwt-secret-key-change-in-production")
	if cfg.JWTSecretKey != "" {
		secretKey = []byte(cfg.JWTSecretKey)
	}

	return &JWTManager{
		secretKey:     secretKey,
		tokenDuration: 5 * time.Minute, // 5-minute tokens as per requirements
	}
}

// GenerateToken generates a new Registry JWT token
func (j *JWTManager) GenerateTokenResponse(ctx context.Context, claims JWTClaims) (*TokenResponse, error) {
	if claims.IssuedAt == nil {
		claims.IssuedAt = jwt.NewNumericDate(time.Now())
	}
	if claims.ExpiresAt == nil {
		claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(j.tokenDuration))
	}
	if claims.NotBefore == nil {
		claims.NotBefore = jwt.NewNumericDate(time.Now())
	}
	if claims.Issuer == "" {
		claims.Issuer = "mcp-registry"
	}

	// Create token with claims
	token := jwt.NewWithClaims(&jwt.SigningMethodEd25519{}, claims)

	// Sign token
	tokenString, err := token.SignedString(j.secretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign token: %w", err)
	}

	return &TokenResponse{
		RegistryToken: tokenString,
		ExpiresAt:     int(claims.ExpiresAt.Unix()),
	}, nil
}

// ValidateToken validates a Registry JWT token and returns the claims
func (j *JWTManager) ValidateToken(ctx context.Context, tokenString string) (*JWTClaims, error) {
	// Parse token
	// This also validates expiry
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v, expected Ed25519", token.Header["alg"])
		}
		return j.secretKey, nil
	})

	// Validate token
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	// Extract claims
	claims, ok := token.Claims.(*JWTClaims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}

func (j *JWTManager) HasPermission(resource string, action PermissionAction, permissions []Permission) bool {
	for _, perm := range permissions {
		if perm.Action == action && isResourceMatch(resource, perm.ResourcePattern) {
			return true
		}
	}
	return false
}

// * should match anything
// something* should match something.com etc.
// io.github.domdomegg/* should match io.github.domdomegg/myrepo
func isResourceMatch(resource, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(resource, strings.TrimSuffix(pattern, "*"))
	}
	return resource == pattern
}
