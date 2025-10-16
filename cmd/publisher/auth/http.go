package auth

type HTTPProvider struct {
	*CryptoProvider
}

// NewHTTPProvider creates a new HTTP-based auth provider
func NewHTTPProvider(registryURL, domain, privateKey string, cryptoAlgorithm CryptoAlgorithm) Provider {
	return &HTTPProvider{
		CryptoProvider: &CryptoProvider{
			registryURL:     registryURL,
			domain:          domain,
			privateKey:      privateKey,
			cryptoAlgorithm: cryptoAlgorithm,
			authMethod:      "http",
		},
	}
}

// Name returns the name of this auth provider
func (h *HTTPProvider) Name() string {
	return "http"
}
