package remote

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIdentityEncryptSignRoundTrip(t *testing.T) {
	sender, err := NewIdentity()
	require.NoError(t, err)
	receiver, err := NewIdentity()
	require.NoError(t, err)
	recipient, err := receiver.Recipient()
	require.NoError(t, err)

	payload := SearchRequest{RequestID: "request-1", Query: "report", Mode: "file", Limit: 50}
	ciphertext, err := sender.EncryptJSON(recipient, payload)
	require.NoError(t, err)

	envelope := Envelope{
		Version: ProtocolVersion, ID: "event-1", Kind: "search.request",
		SourceDevice: "source", TargetDevice: "target", Ciphertext: ciphertext,
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute),
	}
	require.NoError(t, sender.SignEnvelope(&envelope))
	publicKey, err := sender.SigningPublicKey()
	require.NoError(t, err)
	require.NoError(t, VerifyEnvelope(envelope, publicKey))

	var decoded SearchRequest
	require.NoError(t, receiver.DecryptJSON(envelope.Ciphertext, &decoded))
	require.Equal(t, payload, decoded)

	envelope.Ciphertext += "tampered"
	require.ErrorIs(t, VerifyEnvelope(envelope, publicKey), ErrInvalidSignature)
}

func TestWrongIdentityCannotDecrypt(t *testing.T) {
	sender, _ := NewIdentity()
	receiver, _ := NewIdentity()
	wrong, _ := NewIdentity()
	recipient, _ := receiver.Recipient()
	ciphertext, err := sender.EncryptJSON(recipient, map[string]string{"secret": "value"})
	require.NoError(t, err)
	var decoded map[string]string
	require.Error(t, wrong.DecryptJSON(ciphertext, &decoded))
	require.False(t, errors.Is(wrong.DecryptJSON(ciphertext, &decoded), ErrInvalidSignature))
}
