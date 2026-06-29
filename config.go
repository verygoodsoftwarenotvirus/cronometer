package cronometer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Environment variables that override the config file's credentials. Handy with `op run` so the
// password never has to touch disk.
const (
	EnvEmail = "CRONOMETER_EMAIL"
	//nolint:gosec // G101: this is the NAME of an environment variable, not a credential value.
	EnvPassword = "CRONOMETER_PASSWORD"
	//nolint:gosec // G101: this is the NAME of an environment variable, not a credential value.
	EnvTOTPSecret = "CRONOMETER_TOTP_SECRET"
)

// Config holds Cronometer credentials.
//
// NOTE: the on-disk config file stores the password in plaintext. Protect it with file
// permissions (it is written 0600), or skip the file entirely and supply credentials via the
// CRONOMETER_EMAIL / CRONOMETER_PASSWORD environment variables (e.g. sourced from 1Password or
// SOPS).
type Config struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	// TOTPSecret is the optional base32 2FA secret (from authenticator setup). When set, crono
	// generates the 6-digit code itself instead of prompting. Storing it here means the file's
	// secrecy is your only protection — guard it like the password.
	TOTPSecret string `json:"totp_secret,omitempty"`
}

// configDir returns crono's config directory: $XDG_CONFIG_HOME/crono, falling back to
// ~/.config/crono.
func configDir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "crono"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".config", "crono"), nil
}

// DefaultConfigPath returns the default config file location.
func DefaultConfigPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// DefaultSessionPath returns the default cached-session file location.
func DefaultSessionPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

// LoadConfig reads a Config from a JSON file. String fields are run through environment-variable
// expansion, so a value like "$CRONOMETER_PASSWORD" or "${MY_SECRET}" is replaced with the
// variable's value (undefined variables expand to empty). This lets the file be committed without
// secrets. Note: a literal "$" in a value is treated as a variable reference — keep such values
// in the environment instead.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err = json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	cfg.Email = os.ExpandEnv(cfg.Email)
	cfg.Password = os.ExpandEnv(cfg.Password)
	cfg.TOTPSecret = os.ExpandEnv(cfg.TOTPSecret)
	return &cfg, nil
}

// SaveConfig writes a Config to a JSON file with owner-only permissions, creating parent
// directories as needed.
func SaveConfig(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err = os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// ResolveCredentials loads credentials from the config file at path (if it exists) and then
// applies any CRONOMETER_EMAIL / CRONOMETER_PASSWORD environment overrides. A missing file is
// fine as long as both credentials end up set via the environment. It errors if either
// credential is ultimately empty.
func ResolveCredentials(path string) (*Config, error) {
	cfg := &Config{}
	if loaded, err := LoadConfig(path); err == nil {
		cfg = loaded
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	if v := os.Getenv(EnvEmail); v != "" {
		cfg.Email = v
	}
	if v := os.Getenv(EnvPassword); v != "" {
		cfg.Password = v
	}
	if v := os.Getenv(EnvTOTPSecret); v != "" {
		cfg.TOTPSecret = v
	}

	switch {
	case cfg.Email == "" && cfg.Password == "":
		return nil, fmt.Errorf("no credentials: set %s/%s or create %s", EnvEmail, EnvPassword, path)
	case cfg.Email == "":
		return nil, fmt.Errorf("missing email: set %s or add it to %s", EnvEmail, path)
	case cfg.Password == "":
		return nil, fmt.Errorf("missing password: set %s or add it to %s", EnvPassword, path)
	}

	return cfg, nil
}
