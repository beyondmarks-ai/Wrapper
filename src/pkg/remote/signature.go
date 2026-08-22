package remote

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
)

func (i Identity) Sign(message []byte) (string, error) {
	privateKey, err := i.signingPrivateKey()
	if err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, message)), nil
}

func VerifySignature(message []byte, signature, signingKey string) error {
	publicKey, err := base64.RawStdEncoding.DecodeString(signingKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("decode signing key: %w", ErrInvalidSignature)
	}
	decodedSignature, err := base64.RawStdEncoding.DecodeString(signature)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(publicKey), message, decodedSignature) {
		return ErrInvalidSignature
	}
	return nil
}
