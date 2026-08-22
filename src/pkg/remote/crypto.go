package remote

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"filippo.io/age"
)

var ErrInvalidSignature = errors.New("invalid device signature")

type Identity struct {
	AgeIdentity string `json:"ageIdentity"`
	SignPrivate string `json:"signPrivate"`
}

func NewIdentity() (Identity, error) {
	ageIdentity, err := age.GenerateX25519Identity()
	if err != nil {
		return Identity{}, fmt.Errorf("generate encryption identity: %w", err)
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Identity{}, fmt.Errorf("generate signing identity: %w", err)
	}
	return Identity{
		AgeIdentity: ageIdentity.String(),
		SignPrivate: base64.RawStdEncoding.EncodeToString(privateKey),
	}, nil
}

func (i Identity) Recipient() (string, error) {
	identity, err := age.ParseX25519Identity(i.AgeIdentity)
	if err != nil {
		return "", fmt.Errorf("parse encryption identity: %w", err)
	}
	return identity.Recipient().String(), nil
}

func (i Identity) SigningPublicKey() (string, error) {
	privateKey, err := i.signingPrivateKey()
	if err != nil {
		return "", err
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	return base64.RawStdEncoding.EncodeToString(publicKey), nil
}

func (i Identity) EncryptJSON(recipient string, value any) (string, error) {
	parsedRecipient, err := age.ParseX25519Recipient(recipient)
	if err != nil {
		return "", fmt.Errorf("parse recipient: %w", err)
	}
	plain, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal encrypted payload: %w", err)
	}
	var encrypted bytes.Buffer
	writer, err := age.Encrypt(&encrypted, parsedRecipient)
	if err != nil {
		return "", fmt.Errorf("start payload encryption: %w", err)
	}
	if _, err = writer.Write(plain); err != nil {
		return "", fmt.Errorf("encrypt payload: %w", err)
	}
	if err = writer.Close(); err != nil {
		return "", fmt.Errorf("finish payload encryption: %w", err)
	}
	return base64.RawStdEncoding.EncodeToString(encrypted.Bytes()), nil
}

func (i Identity) DecryptJSON(ciphertext string, value any) error {
	identity, err := age.ParseX25519Identity(i.AgeIdentity)
	if err != nil {
		return fmt.Errorf("parse encryption identity: %w", err)
	}
	encrypted, err := base64.RawStdEncoding.DecodeString(ciphertext)
	if err != nil {
		return fmt.Errorf("decode encrypted payload: %w", err)
	}
	reader, err := age.Decrypt(bytes.NewReader(encrypted), identity)
	if err != nil {
		return fmt.Errorf("decrypt payload: %w", err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read decrypted payload: %w", err)
	}
	if err = json.Unmarshal(plain, value); err != nil {
		return fmt.Errorf("unmarshal decrypted payload: %w", err)
	}
	return nil
}

func (i Identity) SignEnvelope(envelope *Envelope) error {
	privateKey, err := i.signingPrivateKey()
	if err != nil {
		return err
	}
	signed, err := envelope.signingBytes()
	if err != nil {
		return err
	}
	envelope.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, signed))
	return nil
}

func VerifyEnvelope(envelope Envelope, signingKey string) error {
	publicKey, err := base64.RawStdEncoding.DecodeString(signingKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("decode signing key: %w", ErrInvalidSignature)
	}
	signature, err := base64.RawStdEncoding.DecodeString(envelope.Signature)
	if err != nil {
		return fmt.Errorf("decode signature: %w", ErrInvalidSignature)
	}
	signed, err := envelope.signingBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), signed, signature) {
		return ErrInvalidSignature
	}
	return nil
}

func (i Identity) signingPrivateKey() (ed25519.PrivateKey, error) {
	privateKey, err := base64.RawStdEncoding.DecodeString(i.SignPrivate)
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("decode signing identity: %w", err)
	}
	return ed25519.PrivateKey(privateKey), nil
}

func (e Envelope) signingBytes() ([]byte, error) {
	e.Signature = ""
	return json.Marshal(e)
}
