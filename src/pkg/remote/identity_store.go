package remote

import (
	"encoding/json"
	"fmt"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/securestore"
)

func SaveIdentity(path string, identity Identity) error {
	plain, err := json.Marshal(identity)
	if err != nil {
		return fmt.Errorf("marshal device identity: %w", err)
	}
	if err = securestore.Write(path, plain); err != nil {
		return fmt.Errorf("protect device identity: %w", err)
	}
	return nil
}

func LoadIdentity(path string) (Identity, error) {
	plain, err := securestore.Read(path)
	if err != nil {
		return Identity{}, fmt.Errorf("read protected identity: %w", err)
	}
	var identity Identity
	if err = json.Unmarshal(plain, &identity); err != nil {
		return Identity{}, fmt.Errorf("decode protected identity: %w", err)
	}
	if _, err = identity.Recipient(); err != nil {
		return Identity{}, err
	}
	if _, err = identity.SigningPublicKey(); err != nil {
		return Identity{}, err
	}
	return identity, nil
}
