package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/polarn/anjin-intel/internal/config"
	"github.com/polarn/anjin-intel/internal/ship"
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

// install copies the running binary to a stable per-user path, saves the config,
// and registers a systemd user unit that runs it at login. Idempotent.
func install(args []string) error {
	if runtime.GOOS != "linux" {
		return errors.New("`install` is Linux-only for now — run `anjin-intel run …` directly on macOS/Windows")
	}
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	server := fs.String("server", "", "anjin server base URL")
	token := fs.String("token", "", "enrollment token from the Intel tab")
	logdir := fs.String("logdir", "", "EVE Chatlogs directory (auto-detected if omitted)")
	channels := fs.String("channels", "", "optional comma-separated channel seed (the Intel tab is authoritative)")
	binDir := fs.String("bin-dir", "", "where to install the binary (default ~/.local/bin)")
	fs.Parse(args)

	if *server == "" || *token == "" {
		return errors.New("--server and --token are required")
	}
	ld := *logdir
	if ld == "" {
		if ld = detectLogdir(); ld == "" {
			return errors.New("could not find the EVE Chatlogs directory — pass --logdir")
		}
		fmt.Printf("detected logdir: %s\n", ld)
	}
	if _, err := os.Stat(ld); err != nil {
		return fmt.Errorf("logdir %q: %w", ld, err)
	}

	bd := *binDir
	if bd == "" {
		var err error
		if bd, err = config.DefaultBinDir(); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(bd, 0o755); err != nil {
		return err
	}
	bin := filepath.Join(bd, "anjin-intel")
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if self != bin {
		if err := copyFile(self, bin); err != nil {
			return fmt.Errorf("install binary: %w", err)
		}
	}

	if err := (config.Config{Server: *server, Token: *token, Logdir: ld, Channels: splitList(*channels), Bin: bin}).Save(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	unitPath, err := config.UnitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(unitPath, []byte(fmt.Sprintf(unitFmt, bin)), 0o644); err != nil {
		return err
	}

	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	if err := systemctl("enable", unitName); err != nil {
		return err
	}
	if err := systemctl("restart", unitName); err != nil { // start, or restart to pick up a re-install
		return err
	}

	cfgPath, _ := config.Path()
	fmt.Printf("installed.\n  binary:    %s\n  config:    %s\n  autostart: %s\nRunning now and at login. Channels are managed in the Intel tab.\n", bin, cfgPath, unitPath)
	return nil
}

// uninstall stops + removes the autostart unit and deletes the binary + config.
func uninstall(args []string) error {
	if runtime.GOOS != "linux" {
		return errors.New("`uninstall` is Linux-only")
	}
	_ = systemctl("disable", "--now", unitName) // best-effort
	if unitPath, err := config.UnitPath(); err == nil {
		os.Remove(unitPath)
	}
	_ = systemctl("daemon-reload")

	cfg, _ := config.Load()
	if cfg.Bin != "" {
		os.Remove(cfg.Bin)
	}
	if d, err := config.Dir(); err == nil {
		os.RemoveAll(d)
	}
	fmt.Println("uninstalled: autostart removed, binary + config deleted.")
	return nil
}

// status reports config, autostart state, server reachability, and last ship.
func status(_ []string) error {
	cfg, err := config.Load()
	if err != nil {
		fmt.Println("not installed (no config). run: anjin-intel install --server <url> --token <tok>")
		return nil
	}
	fmt.Printf("server:    %s\n", cfg.Server)
	fmt.Printf("logdir:    %s\n", cfg.Logdir)
	if len(cfg.Channels) > 0 {
		fmt.Printf("seed:      %s\n", strings.Join(cfg.Channels, ", "))
	}
	if runtime.GOOS == "linux" {
		fmt.Printf("autostart: %s\n", systemctlOut("is-active", unitName))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := ship.New(cfg.Server, cfg.Token).Allowlist(ctx); err != nil {
		fmt.Printf("server:    unreachable / token? (%v)\n", err)
	} else {
		fmt.Println("reach:     server reachable, token OK")
	}
	if st, err := config.LoadState(); err == nil && !st.LastShip.IsZero() {
		fmt.Printf("last ship: %s ago\n", time.Since(st.LastShip).Round(time.Second))
	} else {
		fmt.Println("last ship: never (no intel shipped yet)")
	}
	return nil
}

// --- helpers ---

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

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst) // atomic; replacing a running binary is fine on Linux
}

// detectLogdir best-effort finds the EVE Chatlogs dir across common Linux layouts
// (native, Steam/Proton, Lutris/Faugus), preferring the most recently written one.
func detectLogdir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	globs := []string{
		filepath.Join(home, "Documents/EVE/logs/Chatlogs"),
		filepath.Join(home, ".steam/steam/steamapps/compatdata/*/pfx/drive_c/users/steamuser/Documents/EVE/logs/Chatlogs"),
		filepath.Join(home, ".local/share/Steam/steamapps/compatdata/*/pfx/drive_c/users/steamuser/Documents/EVE/logs/Chatlogs"),
		filepath.Join(home, "*/drive_c/users/*/Documents/EVE/logs/Chatlogs"),
		filepath.Join(home, "Games/*/drive_c/users/*/Documents/EVE/logs/Chatlogs"),
	}
	var best string
	var bestMod time.Time
	for _, g := range globs {
		matches, _ := filepath.Glob(g)
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil || !info.IsDir() {
				continue
			}
			if info.ModTime().After(bestMod) {
				best, bestMod = m, info.ModTime()
			}
		}
	}
	return best
}
