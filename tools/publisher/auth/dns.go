package auth

type DNSProvider struct {
	*CryptoProvider
}

// NewDNSProvider creates a new DNS-based auth provider
//
//nolint:ireturn // Factory function intentionally returns interface for dependency injection
func NewDNSProvider(registryURL, domain, hexSeed string) Provider {
	return &DNSProvider{
		CryptoProvider: &CryptoProvider{
			registryURL: registryURL,
			domain:      domain,
			hexSeed:     hexSeed,
			authMethod:  "dns",
		},
	}
}

// Name returns the name of this auth provider
func (d *DNSProvider) Name() string {
	return "dns"
}
