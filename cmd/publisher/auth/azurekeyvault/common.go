package azurekeyvault

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha512"
	"fmt"
	"math/big"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azkeys"
	"github.com/modelcontextprotocol/registry/cmd/publisher/auth"
)

func GetSignatureProvider(vaultName, keyName string) (auth.Signer, error) {
	if vaultName == "" {
		return nil, fmt.Errorf("--vault option (vault name) is required")
	}

	if keyName == "" {
		return nil, fmt.Errorf("--key option (key name) is required")
	}

	return Signer{
		vaultName: vaultName,
		keyName:   keyName,
	}, nil
}

type Signer struct {
	vaultName string
	keyName   string
}

func (d Signer) GetSignedTimestamp(ctx context.Context) (*string, []byte, error) {
	fmt.Fprintf(os.Stdout, "Signing using Azure Key Vault %s and key %s\n", d.vaultName, d.keyName)

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("authentication to Azure failed: %w", err)
	}

	vaultURL := fmt.Sprintf("https://%s.vault.azure.net/", d.vaultName)
	client, err := azkeys.NewClient(vaultURL, cred, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Key Vault client: %w", err)
	}

	keyResp, err := client.GetKey(ctx, d.keyName, "", nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to retrieve key for public parameters: %w", err)
	}

	if *keyResp.Key.Kty != azkeys.KeyTypeEC && *keyResp.Key.Kty != azkeys.KeyTypeECHSM {
		return nil, nil, fmt.Errorf("unsupported key type: kty=%v (only EC keys are supported)", keyResp.Key.Kty)
	}

	if *keyResp.Key.Crv != azkeys.CurveNameP384 {
		return nil, nil, fmt.Errorf("unsupported curve: crv=%v (only P-384 is supported)", keyResp.Key.Crv)
	}

	fmt.Fprintln(os.Stdout, "Successfully read the public key from Key Vault.")
	auth.PrintEcdsaP384KeyInfo(ecdsa.PublicKey{
		Curve: elliptic.P384(),
		X:     new(big.Int).SetBytes(keyResp.Key.X),
		Y:     new(big.Int).SetBytes(keyResp.Key.Y),
	})

	timestamp := auth.GetTimestamp()
	digest := sha512.Sum384([]byte(timestamp))
	alg := azkeys.SignatureAlgorithmES384
	fmt.Fprintln(os.Stdout, "Executing the sign request...")
	signResp, err := client.Sign(ctx, d.keyName, "", azkeys.SignParameters{
		Algorithm: &alg,
		Value:     digest[:],
	}, nil)

	if err != nil {
		return nil, nil, fmt.Errorf("failed to sign message: %w", err)
	}

	return &timestamp, signResp.Result, nil
}
