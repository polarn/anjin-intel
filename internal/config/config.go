// Package config is the shipper's per-user state: the install config (server, token,
// logdir, channel seed, installed binary path) and a tiny heartbeat (last successful
// ship). Lives under the XDG config dir (~/.config/anjin-intel), config.json mode
// 0600 since it holds the enrollment token.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const appDir = "anjin-intel"

// Config is what `install` writes and `run` reads when flags are absent (so the
// autostart unit can invoke `anjin-intel run` with no arguments).
type Config struct {
	Server   string   `json:"server"`
	Token    string   `json:"token"`
	Logdir   string   `json:"logdir"`
	Channels []string `json:"channels,omitempty"` // optional offline/first-run seed
	Bin      string   `json:"bin,omitempty"`      // installed binary path (for uninstall)
}

// Dir is the per-user config directory (~/.config/anjin-intel).
func Dir() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, appDir), nil
}

// Path is the config file (~/.config/anjin-intel/config.json).
func Path() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

// Load reads the saved config.
func Load() (Config, error) {
	p, err := Path()
	if err != nil {
		return Config{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return Config{}, err
	}
	var c Config
	return c, json.Unmarshal(b, &c)
}

// Save writes the config 0600 (it holds the token).
func (c Config) Save() error {
	d, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	p, _ := Path()
	return os.WriteFile(p, b, 0o600)
}

// State is the heartbeat `status` reads to show when intel last shipped.
type State struct {
	LastShip time.Time `json:"last_ship"`
}

func statePath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "state.json"), nil
}

// LoadState reads the heartbeat (zero State if absent).
func LoadState() (State, error) {
	p, err := statePath()
	if err != nil {
		return State{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return State{}, err
	}
	var s State
	return s, json.Unmarshal(b, &s)
}

// SaveState writes the heartbeat (best-effort; callers ignore the error).
func SaveState(s State) error {
	d, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(s)
	p, _ := statePath()
	return os.WriteFile(p, b, 0o600)
}

// DefaultBinDir is where `install` copies the binary (~/.local/bin).
func DefaultBinDir() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".local", "bin"), nil
}

// UnitPath is the systemd user unit (~/.config/systemd/user/anjin-intel.service).
func UnitPath() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "systemd", "user", "anjin-intel.service"), nil
}
