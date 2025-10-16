package auth

type DNSProvider struct {
	*CryptoProvider
}

// NewDNSProvider creates a new DNS-based auth provider
func NewDNSProvider(registryURL, domain, privateKey string, cryptoAlgorithm CryptoAlgorithm) Provider {
	return &DNSProvider{
		CryptoProvider: &CryptoProvider{
			registryURL:     registryURL,
			domain:          domain,
			privateKey:      privateKey,
			cryptoAlgorithm: cryptoAlgorithm,
			authMethod:      "dns",
		},
	}
}

// Name returns the name of this auth provider
func (d *DNSProvider) Name() string {
	return "dns"
}
