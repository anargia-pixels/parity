package parity

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestStateConcurrentUpdates(t *testing.T) {
	stateDir := t.TempDir()
	var workers sync.WaitGroup
	for index := 0; index < 20; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			label := fmt.Sprintf("entry-%d", index)
			if err := writeState(context.Background(), stateDir, map[string]entryState{label: {LastSync: "now", Result: "ok"}}); err != nil {
				t.Errorf("write state: %v", err)
			}
		}(index)
	}
	workers.Wait()
	state, err := readState(stateDir)
	if err != nil || len(state.Entries) != 20 || state.Updated == "" {
		t.Fatalf("lost updates: %#v, %v", state, err)
	}
}

func TestStateRecoversCorruptFile(t *testing.T) {
	stateDir := t.TempDir()
	writeTestFile(t, filepath.Join(stateDir, "state.json"), "not JSON")
	if err := writeState(context.Background(), stateDir, map[string]entryState{"editor": {Result: "skipped", Message: "empty"}}); err != nil {
		t.Fatal(err)
	}
	state, err := readState(stateDir)
	if err != nil || state.Entries["editor"].Message != "empty" {
		t.Fatalf("recovered state: %#v, %v", state, err)
	}
}
