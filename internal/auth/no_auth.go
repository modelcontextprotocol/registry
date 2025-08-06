//go:build noauth

package auth

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/model"
)

func init() {
	fmt.Fprintln(os.Stderr, `WARNING: "noauth" build tag has disabled authentication`)
}

// NewAuthService creates a new authentication service that does nothing. All its methods succeed unconditionally.
func NewAuthService(*config.Config) Service {
	return &noAuth{}
}

type noAuth struct{}

// StartAuthFlow always returns fake flow info and a fake status token
func (*noAuth) StartAuthFlow(context.Context, model.AuthMethod, string) (map[string]string, string, error) {
	return map[string]string{"fake": "info"}, "fake-status-token", nil
}

// CheckAuthStatus always returns a fake token
func (*noAuth) CheckAuthStatus(context.Context, string) (string, error) {
	return "fake-token", nil
}

// ValidateAuth always returns true
func (*noAuth) ValidateAuth(context.Context, model.Authentication) (bool, error) {
	return true, nil
}
