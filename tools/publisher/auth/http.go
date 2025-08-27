package auth

type HTTPProvider struct {
	*CryptoProvider
}

// NewHTTPProvider creates a new HTTP-based auth provider
func NewHTTPProvider(registryURL, domain, hexSeed string) Provider {
	return &HTTPProvider{
		CryptoProvider: &CryptoProvider{
			registryURL: registryURL,
			domain:      domain,
			hexSeed:     hexSeed,
			authMethod:  "http",
		},
	}
}

// Name returns the name of this auth provider
func (h *HTTPProvider) Name() string {
	return "http"
}
