package verification

import (
	"context"
	"fmt"
	"time"
)

// MockDNSResolver implements DNSResolver for testing
type MockDNSResolver struct {
	// TXTRecords maps domain names to their TXT records
	TXTRecords map[string][]string

	// Errors maps domain names to errors that should be returned
	Errors map[string]error

	// Delay simulates DNS query latency
	Delay time.Duration

	// CallCount tracks how many times LookupTXT was called
	CallCount int

	// LastDomain tracks the last domain that was queried
	LastDomain string
}

func (m *MockDNSResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	m.CallCount++
	m.LastDomain = name

	// Simulate delay if configured
	if m.Delay > 0 {
		timer := time.NewTimer(m.Delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Return error if configured for this domain
	if err, exists := m.Errors[name]; exists {
		return nil, err
	}

	// Return TXT records if configured
	if records, exists := m.TXTRecords[name]; exists {
		return records, nil
	}

	// Default: return empty records (domain exists but no TXT records)
	return []string{}, nil
}

// Reset clears all state in the mock resolver
func (m *MockDNSResolver) Reset() {
	m.CallCount = 0
	m.LastDomain = ""
	if m.TXTRecords != nil {
		for k := range m.TXTRecords {
			delete(m.TXTRecords, k)
		}
	}
	if m.Errors != nil {
		for k := range m.Errors {
			delete(m.Errors, k)
		}
	}
}

// SetTXTRecord sets a TXT record for a domain
func (m *MockDNSResolver) SetTXTRecord(domain string, records ...string) {
	if m.TXTRecords == nil {
		m.TXTRecords = make(map[string][]string)
	}
	m.TXTRecords[domain] = records
}

// SetError sets an error to be returned for a domain
func (m *MockDNSResolver) SetError(domain string, err error) {
	if m.Errors == nil {
		m.Errors = make(map[string]error)
	}
	m.Errors[domain] = err
}

// SetVerificationToken is a convenience method to set up a valid verification token
func (m *MockDNSResolver) SetVerificationToken(domain, token string) {
	m.SetTXTRecord(domain, fmt.Sprintf("mcp-verify=%s", token))
}

// NewMockDNSResolver creates a new mock DNS resolver
func NewMockDNSResolver() *MockDNSResolver {
	return &MockDNSResolver{
		TXTRecords: make(map[string][]string),
		Errors:     make(map[string]error),
	}
}
