package cloudstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
)

const MaxSharedRoots = 64

type State struct {
	Version      int           `json:"version"`
	APIURL       string        `json:"apiUrl"`
	Device       remote.Device `json:"device"`
	SharedRoots  []string      `json:"sharedRoots"`
	DownloadDir  string        `json:"downloadDir"`
	IdentityPath string        `json:"identityPath"`
}

func Load(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var state State
	if err = json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("decode cloud device state: %w", err)
	}
	if state.Version != 1 || state.Device.ID == "" || state.APIURL == "" {
		return State{}, errors.New("cloud device state is incomplete; run 'wrap device register'")
	}
	return state, nil
}

func Save(path string, state State) error {
	state.Version = 1
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".cloud-state-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath) //nolint:errcheck
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(data)
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = commitFile(temporaryPath, path); err != nil {
		return fmt.Errorf("commit cloud device state: %w", err)
	}
	return nil
}

func AddSharedRoot(state *State, root string) error {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("shared root must be a directory")
	}
	absolute = filepath.Clean(absolute)
	for _, existing := range state.SharedRoots {
		if strings.EqualFold(existing, absolute) {
			return nil
		}
	}
	if len(state.SharedRoots) >= MaxSharedRoots {
		return fmt.Errorf("at most %d shared roots are allowed", MaxSharedRoots)
	}
	state.SharedRoots = append(state.SharedRoots, absolute)
	return nil
}

func RemoveSharedRoot(state *State, root string) error {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	for index, existing := range state.SharedRoots {
		if strings.EqualFold(existing, filepath.Clean(absolute)) {
			state.SharedRoots = append(state.SharedRoots[:index], state.SharedRoots[index+1:]...)
			return nil
		}
	}
	return errors.New("shared root was not found")
}
