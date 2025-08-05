package verification

import (
	"context"
	"fmt"
	"net"
)

// DNSResolver interface allows for dependency injection and testing
type DNSResolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// DefaultDNSResolver wraps net.Resolver to implement our interface
type DefaultDNSResolver struct {
	resolver *net.Resolver
}

func (d *DefaultDNSResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return d.resolver.LookupTXT(ctx, name)
}

// NewDefaultDNSResolver creates a DNS resolver with the given configuration
//
//nolint:ireturn // Factory function returning interface is acceptable for dependency injection
func NewDefaultDNSResolver(config *DNSVerificationConfig) DNSResolver {
	if config.UseSecureResolvers && len(config.CustomResolvers) > 0 {
		// Create custom dialer for secure resolvers
		dialer := &net.Dialer{
			Timeout: config.Timeout,
		}

		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				// Use first available custom resolver
				for _, resolverAddr := range config.CustomResolvers {
					conn, err := dialer.DialContext(ctx, network, resolverAddr)
					if err == nil {
						return conn, nil
					}
				}
				return nil, fmt.Errorf("all custom DNS resolvers failed")
			},
		}

		return &DefaultDNSResolver{resolver: resolver}
	}

	// Use system default resolver
	return &DefaultDNSResolver{resolver: net.DefaultResolver}
}
