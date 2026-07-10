// Package state persists last-apply metadata under .pastel/.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const DirName = ".pastel"

// State is written after a successful sync.
type State struct {
	PackCoordinate string    `json:"packCoordinate,omitempty"`
	PackName       string    `json:"packName"`
	PackVersion    string    `json:"packVersion"`
	Minecraft      string    `json:"minecraft,omitempty"`
	Loader         string    `json:"loader,omitempty"` // e.g. Fabric
	ModCount       int       `json:"modCount,omitempty"`
	AppliedAt      time.Time `json:"appliedAt"`
	FileCount      int       `json:"fileCount"`
	ServerJar      string    `json:"serverJar,omitempty"`
}

// Dir returns the absolute .pastel directory for a server root.
func Dir(serverRoot string) string {
	return filepath.Join(serverRoot, DirName)
}

// Path returns the state.json path.
func Path(serverRoot string) string {
	return filepath.Join(Dir(serverRoot), "state.json")
}

// PIDPath returns the server pid file path.
func PIDPath(serverRoot string) string {
	return filepath.Join(Dir(serverRoot), "server.pid")
}

// HoldPIDPath is the process that keeps the console FIFO open.
func HoldPIDPath(serverRoot string) string {
	return filepath.Join(Dir(serverRoot), "hold.pid")
}

// ConsoleInPath is the FIFO used to send console commands to the server.
func ConsoleInPath(serverRoot string) string {
	return filepath.Join(Dir(serverRoot), "console.in")
}

// ConsoleLogPath is where the server's stdout/stderr is captured.
func ConsoleLogPath(serverRoot string) string {
	return filepath.Join(Dir(serverRoot), "console.log")
}

// Load reads state or returns nil if missing.
func Load(serverRoot string) (*State, error) {
	data, err := os.ReadFile(Path(serverRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Save writes state.json.
func Save(serverRoot string, s *State) error {
	if err := os.MkdirAll(Dir(serverRoot), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(Path(serverRoot), data, 0o644)
}
