package googlekms

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha512"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"

	kms "cloud.google.com/go/kms/apiv1"
	"cloud.google.com/go/kms/apiv1/kmspb"
	"github.com/modelcontextprotocol/registry/cmd/publisher/auth"
)

// GetSignatureProvider validates inputs and returns a GoogleKMSSigner implementing auth.Signer.
func GetSignatureProvider(resourceName string) (auth.Signer, error) {
	if resourceName == "" {
		return nil, fmt.Errorf("--resource is required, e.g. projects/my-project/locations/global/keyRings/fellowship/cryptoKeys/bilbo/cryptoKeyVersions/1")
	}

	return &Signer{
		resource: resourceName,
	}, nil
}

type Signer struct {
	resource string
}

type ecdsaSignature struct {
	R, S *big.Int
}

func derToRS(der []byte, curve elliptic.Curve) ([]byte, error) {
	var sig ecdsaSignature
	if _, err := asn1.Unmarshal(der, &sig); err != nil {
		return nil, fmt.Errorf("invalid DER ECDSA signature: %w", err)
	}
	if sig.R == nil || sig.S == nil || sig.R.Sign() <= 0 || sig.S.Sign() <= 0 {
		return nil, fmt.Errorf("invalid ECDSA signature components")
	}

	size := (curve.Params().BitSize + 7) / 8
	rBytes := sig.R.Bytes()
	sBytes := sig.S.Bytes()
	if len(rBytes) > size || len(sBytes) > size {
		return nil, fmt.Errorf("ECDSA signature component too large")
	}

	out := make([]byte, 2*size)
	copy(out[size-len(rBytes):size], rBytes)
	copy(out[2*size-len(sBytes):], sBytes)
	return out, nil
}

func (g *Signer) GetSignedTimestamp(ctx context.Context) (*string, []byte, error) {
	client, err := kms.NewKeyManagementClient(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create KMS client: %w", err)
	}
	defer client.Close()

	// Fetch public key (PEM) so we can output expected proof record.
	algo, err := g.showPublicKeyAndGetAlgorithm(ctx, client)
	if err != nil {
		return nil, nil, err
	}

	timestamp := auth.GetTimestamp()

	fmt.Fprintln(os.Stdout, "Executing the sign request...")
	switch algo {
	case auth.AlgorithmEd25519:
		signReq := &kmspb.AsymmetricSignRequest{
			Name: g.resource,
			Data: []byte(timestamp),
		}
		signResp, err := client.AsymmetricSign(ctx, signReq)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to sign with KMS: %w", err)
		}

		return &timestamp, signResp.Signature, nil
	case auth.AlgorithmECDSAP384:
		digest := sha512.Sum384([]byte(timestamp))
		signReq := &kmspb.AsymmetricSignRequest{
			Name:   g.resource,
			Digest: &kmspb.Digest{Digest: &kmspb.Digest_Sha384{Sha384: digest[:]}},
		}
		signResp, err := client.AsymmetricSign(ctx, signReq)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to sign with KMS: %w", err)
		}

		sigBytes, err := derToRS(signResp.Signature, elliptic.P384())
		if err != nil {
			return nil, nil, fmt.Errorf("failed to convert DER signature: %w", err)
		}

		return &timestamp, sigBytes, nil
	}

	return nil, nil, fmt.Errorf("unsupported algorithm: %s", algo)
}

func (g *Signer) showPublicKeyAndGetAlgorithm(ctx context.Context, client *kms.KeyManagementClient) (auth.CryptoAlgorithm, error) {
	pubResp, err := client.GetPublicKey(ctx, &kmspb.GetPublicKeyRequest{Name: g.resource})
	if err != nil {
		return "", fmt.Errorf("failed to get public key: %w", err)
	}

	block, _ := pem.Decode([]byte(pubResp.Pem))
	if block == nil {
		return "", errors.New("failed to decode PEM public key from KMS")
	}

	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse public key: %w", err)
	}

	switch pubResp.Algorithm { //nolint:exhaustive
	case kmspb.CryptoKeyVersion_EC_SIGN_ED25519:
		pk, ok := parsed.(ed25519.PublicKey)
		if !ok {
			return "", errors.New("KMS reported ED25519 but parsed key is different type")
		}
		auth.PrintEd25519KeyInfo(pk)
		return auth.AlgorithmEd25519, nil
	case kmspb.CryptoKeyVersion_EC_SIGN_P384_SHA384:
		pk, ok := parsed.(*ecdsa.PublicKey)
		if !ok || pk.Curve != elliptic.P384() {
			return "", errors.New("KMS reported P-384 but parsed key mismatch")
		}
		auth.PrintEcdsaP384KeyInfo(*pk)
		return auth.AlgorithmECDSAP384, nil
	default:
		return "", fmt.Errorf("unsupported KMS key algorithm: %s", pubResp.Algorithm.String())
	}
}
