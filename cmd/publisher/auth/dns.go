package auth

type DNSProvider struct {
	*CryptoProvider
}

// NewDNSProvider creates a new DNS-based auth provider
func NewDNSProvider(registryURL, domain string, signer *Signer) Provider {
	return &DNSProvider{
		CryptoProvider: &CryptoProvider{
			registryURL: registryURL,
			domain:      domain,
			signer:      *signer,
			authMethod:  "dns",
		},
	}
}

// Name returns the name of this auth provider
func (d *DNSProvider) Name() string {
	return "dns"
}
