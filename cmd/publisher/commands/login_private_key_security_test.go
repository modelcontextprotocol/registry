package commands_test

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/registry/cmd/publisher/commands"
	"github.com/stretchr/testify/require"
)

func TestLoginCommand_PrivateKeySourcesDoNotLeak(t *testing.T) {
	binary, err := os.Executable()
	require.NoError(t, err)
	for _, algorithm := range []string{"ed25519", "ecdsap384"} {
		var key string
		var verify func([]byte, []byte) bool
		if algorithm == "ed25519" {
			pub, priv, err := ed25519.GenerateKey(rand.Reader)
			require.NoError(t, err)
			key = hex.EncodeToString(priv.Seed())
			verify = func(message, sig []byte) bool { return ed25519.Verify(pub, message, sig) }
		} else {
			priv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
			require.NoError(t, err)
			rawKey, err := priv.Bytes()
			require.NoError(t, err)
			key = hex.EncodeToString(rawKey)
			verify = func(message, sig []byte) bool {
				if len(sig) != 96 {
					return false
				}
				d := sha512.Sum384(message)
				return ecdsa.Verify(&priv.PublicKey, d[:], new(big.Int).SetBytes(sig[:48]), new(big.Int).SetBytes(sig[48:]))
			}
		}
		for _, method := range []string{"dns", "http"} {
			for _, source := range []string{"file", "env"} {
				t.Run(algorithm+"/"+method+"/"+source, func(t *testing.T) {
					var requests atomic.Int32
					var valid atomic.Bool
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests.Add(1)
						var body map[string]string
						if json.NewDecoder(r.Body).Decode(&body) == nil {
							sig, e := hex.DecodeString(body["signed_timestamp"])
							valid.Store(e == nil && len(body) == 3 && body["domain"] == "example.com" && r.URL.Path == "/v0/auth/"+method && verify([]byte(body["timestamp"]), sig))
						}
						// Verify the signature but return no token, so the subprocess cannot
						// overwrite the developer's saved login. Token persistence is covered separately.
						w.WriteHeader(http.StatusUnauthorized)
					}))
					defer srv.Close()
					args := []string{method, "--algorithm", algorithm, "--domain", "example.com", "--registry", srv.URL}
					env := []string{}
					for _, v := range os.Environ() {
						if !strings.HasPrefix(v, "MCP_PRIVATE_KEY=") {
							env = append(env, v)
						}
					}
					if source == "file" {
						p := filepath.Join(t.TempDir(), "key with spaces.hex")
						require.NoError(t, os.WriteFile(p, []byte(key+"\r\n"), 0600))
						args = append(args, "--private-key-file", p)
						env = append(env, "MCP_PRIVATE_KEY=invalid-ambient-key")
					} else {
						env = append(env, "MCP_PRIVATE_KEY=  "+key+"\n")
					}
					cmd := exec.Command(binary, append([]string{"-test.run=^TestLoginCommandPrivateKeyHelperProcess$", "--"}, args...)...) //nolint:gosec // Test executable and fixture arguments, without a shell.
					env = append(env, "MCP_PUBLISHER_TEST_LOGIN=1")
					cmd.Env = env
					out, err := cmd.CombinedOutput()
					require.Error(t, err)
					require.Equal(t, int32(1), requests.Load())
					require.True(t, valid.Load(), "the server must verify the expected key, algorithm and payload")
					require.False(t, strings.Contains(string(out), key), "CLI output must not contain the private key")
				})
			}
		}
	}
	for _, source := range []string{"file", "env"} {
		t.Run("invalid-key/"+source, func(t *testing.T) {
			key := "PRIVATE_KEY_DISCLOSURE_CANARY"
			args := []string{"http", "--domain", "example.com", "--registry", "http://127.0.0.1:1"}
			env := []string{}
			for _, v := range os.Environ() {
				if !strings.HasPrefix(v, "MCP_PRIVATE_KEY=") {
					env = append(env, v)
				}
			}
			if source == "file" {
				p := filepath.Join(t.TempDir(), "key.hex")
				require.NoError(t, os.WriteFile(p, []byte(key), 0600))
				args = append(args, "--private-key-file", p)
			} else {
				env = append(env, "MCP_PRIVATE_KEY="+key)
			}
			cmd := exec.Command(binary, append([]string{"-test.run=^TestLoginCommandPrivateKeyHelperProcess$", "--"}, args...)...) //nolint:gosec // Test executable and fixture arguments, without a shell.
			env = append(env, "MCP_PUBLISHER_TEST_LOGIN=1")
			cmd.Env = env
			out, err := cmd.CombinedOutput()
			require.Error(t, err)
			require.Contains(t, string(out), "invalid hex private key format")
			require.NotContains(t, string(out), key)
		})
	}
}

// Exercise the CLI entry point in a subprocess so stdout/stderr and the key
// environment are isolated. Reuse the test executable, including race
// instrumentation, instead of depending on a separately built publisher.
func TestLoginCommandPrivateKeyHelperProcess(t *testing.T) {
	if os.Getenv("MCP_PUBLISHER_TEST_LOGIN") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator == -1 {
		t.Fatal("missing subprocess argument separator")
	}
	if err := commands.LoginCommand(os.Args[separator+1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestLoginCommand_EmptyPrivateKeyFlagsDoNotFallBack(t *testing.T) {
	binary, err := os.Executable()
	require.NoError(t, err)
	key := strings.Repeat("01", ed25519.SeedSize)
	keyFile := filepath.Join(t.TempDir(), "key.hex")
	require.NoError(t, os.WriteFile(keyFile, []byte(key), 0600))

	for _, method := range []string{"dns", "http"} {
		for _, tc := range []struct {
			name      string
			flags     []string
			wantError string
		}{
			{"empty-key-equals", []string{"--private-key="}, "--private-key must not be empty"},
			{"empty-key-separate", []string{"--private-key", ""}, "--private-key must not be empty"},
			{"whitespace-key", []string{"--private-key", " \t"}, "--private-key must not be empty"},
			{"empty-path-equals", []string{"--private-key-file="}, "--private-key-file must not be empty"},
			{"empty-path-separate", []string{"--private-key-file", ""}, "--private-key-file must not be empty"},
			{"whitespace-path", []string{"--private-key-file", " \t"}, "--private-key-file must not be empty"},
			{"both-empty", []string{"--private-key=", "--private-key-file="}, "not both"},
			{"empty-key-with-file", []string{"--private-key=", "--private-key-file", keyFile}, "not both"},
			{"key-with-empty-path", []string{"--private-key", key, "--private-key-file="}, "not both"},
			{"repeated-key-ends-empty", []string{"--private-key", key, "--private-key="}, "--private-key must not be empty"},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					requests.Add(1)
					w.WriteHeader(http.StatusUnauthorized)
				}))
				defer server.Close()
				args := []string{"-test.run=^TestLoginCommandPrivateKeyHelperProcess$", "--", method, "--domain", "example.com", "--registry", server.URL}
				args = append(args, tc.flags...)
				cmd := exec.Command(binary, args...)
				cmd.Env = append(os.Environ(), "MCP_PRIVATE_KEY="+key, "MCP_PUBLISHER_TEST_LOGIN=1")
				output, err := cmd.CombinedOutput()
				require.Error(t, err)
				require.Contains(t, string(output), tc.wantError)
				require.NotContains(t, string(output), key)
				require.Zero(t, requests.Load(), "invalid explicit flags must fail before any authentication request")
			})
		}
	}
}
