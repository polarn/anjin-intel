package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/polarn/anjin-intel/internal/config"
	"github.com/polarn/anjin-intel/internal/ship"
)

// install copies the running binary to a stable per-user path, saves the config, and
// registers the OS's login launcher (see autostart.go). Idempotent.
//
// The flow is deliberately ordered so the valuable, unprivileged part happens first:
// detect → stop any running copy → copy binary → save config → register autostart. If
// the last step fails or isn't supported, the shipper is still fully configured and
// `anjin-intel run` works with no flags — which is the difference between a partial
// install and a useless one.
func install(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	server := fs.String("server", "", "anjin server base URL")
	token := fs.String("token", "", "enrollment token from the Intel tab")
	logdir := fs.String("logdir", "", "EVE Chatlogs directory (auto-detected if omitted)")
	channels := fs.String("channels", "", "optional comma-separated channel seed (the Intel tab is authoritative)")
	binDir := fs.String("bin-dir", "", "where to install the binary (default ~/.local/bin, or %LOCALAPPDATA%\\Programs\\anjin on Windows)")
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
	// Clear a leftover from a previous overwrite-while-running (see copyFile).
	os.Remove(filepath.Join(bd, config.BinName()+".old"))
	bin := filepath.Join(bd, config.BinName())
	self, err := os.Executable()
	if err != nil {
		return err
	}

	auto := newAutostarter()

	// Windows locks a running image, so a re-install over a live shipper fails unless
	// it is stopped first. No-op on Linux, where the rename-over-running trick works.
	_ = auto.Stop()

	// EqualFold, not !=: Windows paths are case-insensitive, so C:\Users\… and
	// c:\users\… are the same file and copying one onto itself would truncate it.
	if !strings.EqualFold(self, bin) {
		if err := copyFile(self, bin); err != nil {
			return fmt.Errorf("install binary: %w", err)
		}
	}

	if err := (config.Config{Server: *server, Token: *token, Logdir: ld, Channels: splitList(*channels), Bin: bin}).Save(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	cfgPath, _ := config.Path()

	err = auto.Register(bin)
	switch {
	case err == nil:
		fmt.Printf("installed.\n  binary:    %s\n  config:    %s\n  autostart: %s\nRunning now and at login. Channels are managed in the Intel tab.\n",
			bin, cfgPath, auto.State())
		return nil

	case errors.Is(err, errAutostartUnsupported):
		fmt.Printf("installed.\n  binary:    %s\n  config:    %s\nNo autostart backend on this OS yet, so start it yourself (no flags needed — it reads the config):\n  %s run\nChannels are managed in the Intel tab.\n",
			bin, cfgPath, bin)
		return nil

	case errors.Is(err, errElevationDeclined):
		// Everything up to registration succeeded. Say exactly that, and exit non-zero
		// so a script doesn't mistake a half-install for a whole one.
		fmt.Printf("configured, but NOT set to start at login.\n  binary:    %s\n  config:    %s\n\nRegistering a scheduled task needs elevation and the prompt was declined.\nRun the shipper by hand with:\n  %s run\nor re-run `%s install …` and accept the prompt.\n",
			bin, cfgPath, bin, bin)
		return errors.New("autostart not registered (elevation declined)")

	default:
		return fmt.Errorf("register autostart: %w (config was saved; `%s run` still works)", err, bin)
	}
}

// uninstall removes the login entry and deletes the binary + config.
func uninstall(args []string) error {
	auto := newAutostarter()
	autoErr := auto.Remove()

	cfg, _ := config.Load()
	binGone := true
	if cfg.Bin != "" {
		if err := os.Remove(cfg.Bin); err != nil && !os.IsNotExist(err) {
			// Windows refuses to delete a running image — and `uninstall` run FROM the
			// installed copy is exactly that case. The config is gone either way, so
			// the leftover is inert; say where it is rather than claim success.
			binGone = false
			fmt.Printf("could not delete %s: %v\n(stop the shipper, then delete it by hand)\n", cfg.Bin, err)
		}
	}
	if d, err := config.Dir(); err == nil {
		os.RemoveAll(d)
	}

	what := "binary + config deleted"
	if !binGone {
		what = "config deleted; binary left behind (see above)"
	}
	if autoErr != nil {
		if errors.Is(autoErr, errElevationDeclined) {
			fmt.Printf("uninstalled: %s. The login entry was NOT removed (elevation declined) — it will fail harmlessly at next logon; re-run uninstall to clear it.\n", what)
			return nil
		}
		fmt.Printf("uninstalled: %s. Could not remove the login entry: %v\n", what, autoErr)
		return nil
	}
	fmt.Printf("uninstalled: autostart removed, %s.\n", what)
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
	fmt.Printf("autostart: %s\n", newAutostarter().State())

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

// copyFile writes src to dst atomically. On Windows a running .exe cannot be replaced,
// and task termination is asynchronous, so the rename is retried briefly; the caller
// stops the autostart entry first.
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

	var lastErr error
	for i := 0; i < 10; i++ {
		if lastErr = os.Rename(tmp, dst); lastErr == nil {
			return nil
		}
		// Windows allows RENAMING a running image even when it refuses to replace one,
		// so move the old binary aside and retry. The leftover is cleaned up next run.
		if err := os.Rename(dst, dst+".old"); err == nil {
			if lastErr = os.Rename(tmp, dst); lastErr == nil {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	os.Remove(tmp)
	return lastErr
}
