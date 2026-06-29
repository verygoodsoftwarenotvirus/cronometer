package cronoclient

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SaveSession writes a session to path (0600), creating parent directories as needed.
func SaveSession(path string, s Session) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating session directory: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling session: %w", err)
	}
	if err = os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing session: %w", err)
	}
	return nil
}

// LoadSession reads a session from path.
func LoadSession(path string) (Session, error) {
	var s Session
	data, err := os.ReadFile(path)
	if err != nil {
		return s, fmt.Errorf("reading session: %w", err)
	}
	if err = json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("parsing session: %w", err)
	}
	return s, nil
}
