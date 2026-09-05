package auth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha512"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodeECDSAP384Signature_FixedWidth(t *testing.T) {
	// r is small enough that big.Int.Bytes() drops its leading zero bytes. The
	// old append(r.Bytes(), s.Bytes()...) then produced fewer than 96 bytes,
	// which the registry rejects with "invalid signature size for ECDSA P-384".
	r := big.NewInt(1)
	s := new(big.Int).SetBytes(bytes.Repeat([]byte{0xff}, 48))

	sig := encodeECDSAP384Signature(r, s)

	require.Len(t, sig, 96)
	// The verifier splits the signature at byte 48, so a fixed-width encoding
	// must round-trip both components back exactly.
	require.Zero(t, new(big.Int).SetBytes(sig[:48]).Cmp(r))
	require.Zero(t, new(big.Int).SetBytes(sig[48:]).Cmp(s))
}

func TestInProcessSigner_ECDSAP384_SignatureVerifies(t *testing.T) {
	// Fixed, valid P-384 private scalar (well below the group order).
	scalar := bytes.Repeat([]byte{0x11}, 48)
	priv, err := parseRawPrivateKey(elliptic.P384(), scalar)
	require.NoError(t, err)

	signer, err := NewInProcessSigner(hex.EncodeToString(scalar), AlgorithmECDSAP384)
	require.NoError(t, err)

	timestamp, signature, err := signer.GetSignedTimestamp(context.Background())
	require.NoError(t, err)
	require.Len(t, signature, 96)

	digest := sha512.Sum384([]byte(*timestamp))
	r := new(big.Int).SetBytes(signature[:48])
	s := new(big.Int).SetBytes(signature[48:])
	require.True(t, ecdsa.Verify(&priv.PublicKey, digest[:], r, s))
}
