package auth

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
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
	privateKey    ed25519.PrivateKey
	publicKey     ed25519.PublicKey
	tokenDuration time.Duration
}

func NewJWTManager(cfg *config.Config) *JWTManager {
	seed, err := hex.DecodeString(cfg.JWTPrivateKey)
	if err != nil {
		panic(fmt.Sprintf("JWTPrivateKey must be a valid hex-encoded string: %v", err))
	}

	// Require a valid Ed25519 seed (32 bytes)
	if len(seed) != ed25519.SeedSize {
		panic(fmt.Sprintf("JWTPrivateKey seed must be exactly %d bytes for Ed25519, got %d bytes", ed25519.SeedSize, len(seed)))
	}

	// Generate the full Ed25519 key pair from the seed
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)

	return &JWTManager{
		privateKey:    privateKey,
		publicKey:     publicKey,
		tokenDuration: 5 * time.Minute, // 5-minute tokens as per requirements
	}
}

// GenerateToken generates a new Registry JWT token
func (j *JWTManager) GenerateTokenResponse(_ context.Context, claims JWTClaims) (*TokenResponse, error) {
	// Check whether they have global permissions (used by admins)
	hasGlobalPermissions := false
	for _, perm := range claims.Permissions {
		if perm.ResourcePattern == "*" {
			hasGlobalPermissions = true
			break
		}
	}

	// Check permissions against denylist, provided they are not an admin
	if !hasGlobalPermissions {
		for _, blockedNamespace := range BlockedNamespaces {
			if j.HasPermission(blockedNamespace+"/test", PermissionActionPublish, claims.Permissions) {
				return nil, fmt.Errorf("your namespace is blocked. raise an issue at https://github.com/modelcontextprotocol/registry/ if you think this is a mistake")
			}
		}
	}

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

	// Sign token with Ed25519 private key
	tokenString, err := token.SignedString(j.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign token: %w", err)
	}

	return &TokenResponse{
		RegistryToken: tokenString,
		ExpiresAt:     int(claims.ExpiresAt.Unix()),
	}, nil
}

// ValidateToken validates a Registry JWT token and returns the claims
func (j *JWTManager) ValidateToken(_ context.Context, tokenString string) (*JWTClaims, error) {
	// Parse token
	// This also validates expiry
	token, err := jwt.ParseWithClaims(
		tokenString,
		&JWTClaims{},
		func(_ *jwt.Token) (interface{}, error) { return j.publicKey, nil },
		jwt.WithValidMethods([]string{"EdDSA"}),
		jwt.WithExpirationRequired(),
	)

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

func isResourceMatch(resource, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(resource, strings.TrimSuffix(pattern, "*"))
	}
	return resource == pattern
}
