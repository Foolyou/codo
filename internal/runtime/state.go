package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chenan/codo/internal/ids"
)

type State struct {
	RuntimeInstanceID string    `json:"runtime_instance_id"`
	ContainerName     string    `json:"container_name"`
	CreatedAt         time.Time `json:"created_at"`
}

func LoadOrCreateState(path string, runtimeName string) (State, bool, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		var state State
		if err := json.Unmarshal(raw, &state); err != nil {
			return State{}, false, fmt.Errorf("parse state file: %w", err)
		}
		return state, false, nil
	}
	if !os.IsNotExist(err) {
		return State{}, false, fmt.Errorf("read state file: %w", err)
	}

	state := State{
		RuntimeInstanceID: ids.NewRuntimeInstanceID(),
		ContainerName:     runtimeName,
		CreatedAt:         time.Now().UTC(),
	}
	if err := SaveState(path, state); err != nil {
		return State{}, false, err
	}
	return state, true, nil
}

func SaveState(path string, state State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}
	return nil
}
