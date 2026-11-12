package auth_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/modelcontextprotocol/registry/internal/api/handlers/v0/auth"
	intauth "github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
)

func TestHTTPAuthHandler_MultipleKeysSecurity(t *testing.T) {
	cfg := &config.Config{JWTPrivateKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	edPub, edPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	_, unrelatedPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	ecPub, ecPriv := generateECDSAP384KeyPairHTTP(t)
	edRecord := proofRecordFor(edPub)
	ecRecord := fmt.Sprintf("v=MCPv1; k=ecdsap384; p=%s", base64.StdEncoding.EncodeToString(ecPub))
	invalidRecord := "v=MCPv1; k=ed25519; p=AAAA"
	unsupportedRecord := "v=MCPv1; k=unknown; p=AAAA"
	signEd25519 := func(timestamp string) []byte { return ed25519.Sign(edPriv, []byte(timestamp)) }
	signECDSA := func(timestamp string) []byte { return signWithECDSAP384HTTP(t, ecPriv, []byte(timestamp)) }
	signUnrelated := func(timestamp string) []byte { return ed25519.Sign(unrelatedPriv, []byte(timestamp)) }
	// Exercise the production fetcher's body limit with a matching key at the end.
	maxBody := ecRecord + "\n" + edRecord
	maxBody = strings.Repeat(" ", auth.MaxKeyResponseSize-len(maxBody)) + maxBody

	for _, tt := range []struct {
		name      string
		body      string
		sign      func(string) []byte
		timeShift time.Duration
		wantError string
	}{
		{name: "ed25519_after_ecdsa", body: ecRecord + "\n" + edRecord, sign: signEd25519},
		{name: "ecdsa_after_ed25519", body: edRecord + "\n" + ecRecord, sign: signECDSA},
		{name: "invalid_records_before_valid", body: invalidRecord + "\n" + unsupportedRecord + "\n" + edRecord, sign: signEd25519},
		{name: "invalid_record_after_valid", body: edRecord + "\n" + invalidRecord, sign: signEd25519},
		{name: "duplicate_records", body: edRecord + "\n" + edRecord, sign: signEd25519},
		{name: "all_records_invalid", body: invalidRecord + "\n" + unsupportedRecord, sign: signEd25519, wantError: "no valid MCP public key found"},
		{name: "no_key_matches_signature", body: edRecord + "\n" + ecRecord, sign: signUnrelated, wantError: "invalid signature size"},
		{name: "no_ed25519_key_matches_signature", body: edRecord + "\n" + edRecord, sign: signUnrelated, wantError: "signature verification failed"},
		{name: "short_signature", body: edRecord + "\n" + ecRecord, sign: func(string) []byte { return []byte{0} }, wantError: "invalid signature size"},
		{name: "expired_timestamp", body: ecRecord + "\n" + edRecord, sign: signEd25519, timeShift: -time.Minute, wantError: "timestamp outside valid window"},
		{name: "future_timestamp", body: ecRecord + "\n" + edRecord, sign: signEd25519, timeShift: time.Minute, wantError: "timestamp outside valid window"},
		{name: "body_at_limit", body: maxBody, sign: signEd25519},
		{name: "body_above_limit", body: maxBody + "\n", sign: signEd25519, wantError: "HTTP auth key response too large"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, wellKnownPath, r.URL.Path)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			handler := auth.NewHTTPAuthHandler(cfg)
			handler.SetFetcher(auth.NewDefaultHTTPKeyFetcherWithClient(newClientForTLSServer(t, server)))
			timestamp := time.Now().Add(tt.timeShift).UTC().Format(time.RFC3339)
			result, err := handler.ExchangeToken(context.Background(), testDomain, timestamp, hex.EncodeToString(tt.sign(timestamp)))
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				require.Nil(t, result, "failed authentication must not issue a token")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			manager := intauth.NewJWTManager(cfg)
			claims, err := manager.ValidateToken(context.Background(), result.RegistryToken)
			require.NoError(t, err)
			assert.Equal(t, intauth.MethodHTTP, claims.AuthMethod)
			assert.Equal(t, testDomain, claims.AuthMethodSubject)
			require.Len(t, claims.Permissions, 1)
			assert.True(t, manager.HasPermission("com.example/server", intauth.PermissionActionPublish, claims.Permissions))
			assert.False(t, manager.HasPermission("com.other/server", intauth.PermissionActionPublish, claims.Permissions))
			assert.False(t, manager.HasPermission("com.example.sub/server", intauth.PermissionActionPublish, claims.Permissions))
		})
	}
}

func TestHTTPAuthHandler_KeyRetirement(t *testing.T) {
	cfg := &config.Config{JWTPrivateKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	oldPub, oldPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	newPub, newPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	fetcher := &MockHTTPKeyFetcher{keyResponses: map[string]string{
		testDomain: proofRecordFor(oldPub) + "\n" + proofRecordFor(newPub),
	}}
	handler := auth.NewHTTPAuthHandler(cfg)
	handler.SetFetcher(fetcher)
	exchange := func(key ed25519.PrivateKey) (*intauth.TokenResponse, error) {
		timestamp := time.Now().UTC().Format(time.RFC3339)
		return handler.ExchangeToken(context.Background(), testDomain, timestamp, hex.EncodeToString(ed25519.Sign(key, []byte(timestamp))))
	}

	// Both published keys authorize login during rotation, regardless of order.
	oldToken, err := exchange(oldPriv)
	require.NoError(t, err)
	_, err = exchange(newPriv)
	require.NoError(t, err)
	fetcher.keyResponses[testDomain] = proofRecordFor(newPub) + "\n" + proofRecordFor(oldPub)
	_, err = exchange(oldPriv)
	require.NoError(t, err)

	// Removing the old record prevents new logins with that key.
	fetcher.keyResponses[testDomain] = proofRecordFor(newPub)
	result, err := exchange(oldPriv)
	require.ErrorContains(t, err, "signature verification failed")
	require.Nil(t, result)
	_, err = exchange(newPriv)
	require.NoError(t, err)

	// Already issued JWTs are verified independently of the current proof file.
	manager := intauth.NewJWTManager(cfg)
	_, err = manager.ValidateToken(context.Background(), oldToken.RegistryToken)
	require.NoError(t, err)
}

func TestDNSAuthHandler_MultipleKeysInOneTXTRecord(t *testing.T) {
	cfg := &config.Config{JWTPrivateKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	otherPub, otherPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	for _, tt := range []struct {
		name    string
		records []string
		key     ed25519.PrivateKey
		wantErr bool
	}{
		{"second_key_in_same_txt", []string{proofRecordFor(otherPub) + "\n" + proofRecordFor(pub)}, priv, false},
		{"no_matching_key", []string{proofRecordFor(pub) + "\n" + proofRecordFor(pub)}, otherPriv, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler := auth.NewDNSAuthHandler(cfg)
			handler.SetResolver(&MockDNSResolver{txtRecords: map[string][]string{testDomain: tt.records}})
			timestamp := time.Now().UTC().Format(time.RFC3339)
			result, err := handler.ExchangeToken(context.Background(), testDomain, timestamp, hex.EncodeToString(ed25519.Sign(tt.key, []byte(timestamp))))
			if tt.wantErr {
				require.ErrorContains(t, err, "signature verification failed")
				require.Nil(t, result)
				return
			}
			require.NoError(t, err)
			manager := intauth.NewJWTManager(cfg)
			claims, err := manager.ValidateToken(context.Background(), result.RegistryToken)
			require.NoError(t, err)
			assert.Equal(t, intauth.MethodDNS, claims.AuthMethod)
			assert.Equal(t, testDomain, claims.AuthMethodSubject)
			require.Len(t, claims.Permissions, 2)
			assert.True(t, manager.HasPermission("com.example/server", intauth.PermissionActionPublish, claims.Permissions))
			assert.True(t, manager.HasPermission("com.example.sub/server", intauth.PermissionActionPublish, claims.Permissions))
			assert.False(t, manager.HasPermission("com.other/server", intauth.PermissionActionPublish, claims.Permissions))
		})
	}
}
