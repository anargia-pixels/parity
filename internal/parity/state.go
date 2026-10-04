package parity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type entryState struct {
	LastSync string `json:"lastSync"`
	Result   string `json:"result"`
	Message  string `json:"message,omitempty"`
}

type stateFile struct {
	Updated string                `json:"updated"`
	Entries map[string]entryState `json:"entries"`
}

func readState(stateDir string) (stateFile, error) {
	state := stateFile{Entries: make(map[string]entryState)}
	data, err := os.ReadFile(filepath.Join(stateDir, "state.json"))
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if json.Unmarshal(data, &state) != nil || state.Entries == nil {
		state = stateFile{Entries: make(map[string]entryState)}
	}
	return state, nil
}

// writeState locks the merge and replaces the JSON file atomically.
func writeState(ctx context.Context, stateDir string, updates map[string]entryState) error {
	lock, err := acquireFileLock(ctx, filepath.Join(stateDir, "locks", "state.lock"), true)
	if err != nil {
		return fmt.Errorf("lock state file: %w", err)
	}
	defer lock.release()
	state, err := readState(stateDir)
	if err != nil {
		return fmt.Errorf("read state file: %w", err)
	}
	for label, update := range updates {
		state.Entries[label] = update
	}
	state.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	file, err := os.CreateTemp(stateDir, ".state-*.json")
	if err != nil {
		return fmt.Errorf("create state file: %w", err)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(state); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(stateDir, "state.json"))
}

func stateUpdate(outcome syncOutcome) entryState {
	return entryState{LastSync: timestamp(), Result: outcome.status, Message: outcomeMessage(outcome)}
}
