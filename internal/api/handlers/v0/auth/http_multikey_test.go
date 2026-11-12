package auth_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/modelcontextprotocol/registry/internal/api/handlers/v0/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
)

// multiKeyEd25519Seed is a deterministic 32-byte test seed, in the same hex
// format accepted by mcp-publisher login http --private-key.
const multiKeyEd25519Seed = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func proofRecordFor(pub ed25519.PublicKey) string {
	return fmt.Sprintf("v=MCPv1; k=ed25519; p=%s", base64.StdEncoding.EncodeToString(pub))
}

func fingerprintFor(pub ed25519.PublicKey) string {
	info := auth.PublicKeyInfo{Algorithm: auth.AlgorithmEd25519, Key: pub}
	return info.Fingerprint()
}

// multiKeySigningKey derives a deterministic Ed25519 key for these tests.
func multiKeySigningKey(t *testing.T) (ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	seed, err := hex.DecodeString(multiKeyEd25519Seed)
	require.NoError(t, err)
	priv := ed25519.NewKeyFromSeed(seed)
	return priv, priv.Public().(ed25519.PublicKey)
}

func TestParseMCPKeysFromStrings_ExtractsEveryRecord(t *testing.T) {
	_, signingPub := multiKeySigningKey(t)
	stalePub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	staleRec := proofRecordFor(stalePub)
	signingRec := proofRecordFor(signingPub)

	// Rotation: the stale record is still served alongside the matching one.
	inputs := []string{
		staleRec + "\n" + signingRec,
		"unrelated text " + signingRec,
	}
	parsed := auth.ParseMCPKeysFromStrings(inputs)

	got := make(map[string]bool, len(parsed))
	for _, p := range parsed {
		require.NotNil(t, p.PublicKeyInfo, "every matched record must parse as a key")
		got[p.Fingerprint()] = true
	}

	assert.Len(t, parsed, 3,
		"every MCPv1 record in the input must be extracted, not just the first one per string")
	assert.True(t, got[fingerprintFor(stalePub)], "stale record must be among the parsed keys")
	assert.True(t, got[fingerprintFor(signingPub)], "matching record must be among the parsed keys")
}

func TestHTTPAuthHandler_ExchangeToken_MatchingKeyAmongMultiplePublishedRecords(t *testing.T) {
	cfg := &config.Config{
		JWTPrivateKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	handler := auth.NewHTTPAuthHandler(cfg)

	// Derive the signing key from the publisher's 64-hex-character seed format.
	signingKey, signingPub := multiKeySigningKey(t)

	// A stale key from before a rotation, still present in the served body.
	stalePub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	staleRec := proofRecordFor(stalePub)
	signingRec := proofRecordFor(signingPub)

	for _, tt := range []struct {
		name string
		body string
	}{
		{"stale record first", staleRec + "\n" + signingRec},
		{"stale record first, space separated", staleRec + " " + signingRec},
		{"matching record first", signingRec + "\n" + staleRec},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler.SetFetcher(&MockHTTPKeyFetcher{
				keyResponses: map[string]string{testDomain: tt.body},
			})

			// The publisher side: sign the RFC3339 timestamp string, hex-encode.
			timestamp := time.Now().UTC().Format(time.RFC3339)
			signature := ed25519.Sign(signingKey, []byte(timestamp))
			signedTimestamp := hex.EncodeToString(signature)

			result, err := handler.ExchangeToken(context.Background(), testDomain, timestamp, signedTimestamp)
			require.NoError(t, err,
				"a signature made with a key whose proof record is published must verify")
			require.NotNil(t, result)
			assert.NotEmpty(t, result.RegistryToken)
		})
	}
}
