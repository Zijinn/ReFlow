package reminders

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const stateVersion = 1

type Link struct {
	ReminderID   string `json:"reminder_id"`
	ReflowHash   string `json:"reflow_hash"`
	ReminderHash string `json:"reminder_hash"`
}

type ListState struct {
	ListID   string           `json:"list_id"`
	ListName string           `json:"list_name"`
	Links    map[string]*Link `json:"links"`
	LastSync string           `json:"last_sync,omitempty"`
}

type State struct {
	Version int                   `json:"version"`
	Lists   map[string]*ListState `json:"lists"`
}

func DefaultStatePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "reflow", "reminders-sync.json"), nil
}

func loadState(path string) (State, error) {
	state := State{Version: stateVersion, Lists: map[string]*ListState{}}
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(body, &state); err != nil {
		return state, fmt.Errorf("decode %s: %w", path, err)
	}
	state.Version = stateVersion
	if state.Lists == nil {
		state.Lists = map[string]*ListState{}
	}
	for _, list := range state.Lists {
		if list != nil && list.Links == nil {
			list.Links = map[string]*Link{}
		}
	}
	return state, nil
}

func saveState(path string, state State) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(dir, ".reminders-sync-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(body, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
