//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const unitName = "anjin-intel.service"

// unitFmt is the systemd user unit (one %s: the absolute binary path). It runs
// `<bin> run` (which reads the saved config) at login and restarts on crash.
const unitFmt = `[Unit]
Description=anjin-intel — EVE chat-intel shipper
After=network-online.target

[Service]
ExecStart=%s run
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
`

type systemdAutostart struct{}

func newAutostarter() autostarter { return systemdAutostart{} }

// unitPath is the systemd user unit (~/.config/systemd/user/anjin-intel.service).
// It lives here rather than in internal/config because it is systemd-shaped: no other
// backend has a unit file.
func unitPath() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "systemd", "user", unitName), nil
}

func (systemdAutostart) Register(bin string) error {
	p, err := unitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(fmt.Sprintf(unitFmt, bin)), 0o644); err != nil {
		return err
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	if err := systemctl("enable", unitName); err != nil {
		return err
	}
	// start, or restart to pick up a re-install
	return systemctl("restart", unitName)
}

// Stop is a no-op on Linux: replacing a running binary is fine here (install renames
// over it), so there is nothing to halt before the copy.
func (systemdAutostart) Stop() error { return nil }

func (systemdAutostart) Remove() error {
	_ = systemctl("disable", "--now", unitName) // best-effort
	if p, err := unitPath(); err == nil {
		os.Remove(p)
	}
	_ = systemctl("daemon-reload")
	return nil
}

func (systemdAutostart) State() string { return systemctlOut("is-active", unitName) }

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// systemctlOut returns trimmed output regardless of exit code (is-active exits
// non-zero when inactive but still prints the state).
func systemctlOut(args ...string) string {
	out, _ := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	if s := strings.TrimSpace(string(out)); s != "" {
		return s
	}
	return "unknown"
}
