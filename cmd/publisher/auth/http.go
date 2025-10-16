package auth

type HTTPProvider struct {
	*CryptoProvider
}

// NewHTTPProvider creates a new HTTP-based auth provider
func NewHTTPProvider(registryURL, domain string, signer *Signer) Provider {
	return &HTTPProvider{
		CryptoProvider: &CryptoProvider{
			registryURL: registryURL,
			domain:      domain,
			signer:      *signer,
			authMethod:  "http",
		},
	}
}

// Name returns the name of this auth provider
func (h *HTTPProvider) Name() string {
	return "http"
}
