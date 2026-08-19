package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// isolateConfigDir points os.UserConfigDir at a temp dir. Which env var does that
// is per-OS — XDG_CONFIG_HOME on Linux, %AppData% on Windows, $HOME (Library/
// Application Support) on macOS — so setting only the Linux one would silently
// leave these tests reading and writing the developer's real config.
func isolateConfigDir(t *testing.T) {
	t.Helper()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", t.TempDir())
	case "darwin":
		t.Setenv("HOME", t.TempDir())
	default:
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	}
}

func TestConfigRoundTrip(t *testing.T) {
	isolateConfigDir(t)
	c := Config{Server: "https://anjin.example.net", Token: "tok123", Logdir: "/logs", Channels: []string{"Local", "Corp"}, Bin: "/home/x/.local/bin/anjin-intel"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Server != c.Server || got.Token != c.Token || got.Logdir != c.Logdir || got.Bin != c.Bin || len(got.Channels) != 2 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	// The token is sensitive — the file must be 0600. Windows has no POSIX mode
	// bits (Go reports 0666 regardless of what we passed), so assert only where
	// the bits are real; the check must stay strict on Unix.
	if runtime.GOOS == "windows" {
		return
	}
	p, _ := Path()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %o, want 600", info.Mode().Perm())
	}
}

func TestStateRoundTrip(t *testing.T) {
	isolateConfigDir(t)
	now := time.Now().Truncate(time.Second)
	if err := SaveState(State{LastShip: now}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastShip.Equal(now) {
		t.Errorf("LastShip = %v, want %v", got.LastShip, now)
	}
}

func TestBinName(t *testing.T) {
	got := BinName()
	// Windows won't execute a file by name without the suffix, so this is load-bearing
	// rather than cosmetic.
	if runtime.GOOS == "windows" {
		if got != "anjin-intel.exe" {
			t.Errorf("BinName() = %q, want anjin-intel.exe on windows", got)
		}
		return
	}
	if got != "anjin-intel" {
		t.Errorf("BinName() = %q, want anjin-intel", got)
	}
}

func TestDefaultBinDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", `C:\Users\test\AppData\Local`)
		got, err := DefaultBinDir()
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(`C:\Users\test\AppData\Local`, "Programs", "anjin")
		if got != want {
			t.Errorf("DefaultBinDir() = %q, want %q", got, want)
		}
		return
	}
	got, err := DefaultBinDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filepath.ToSlash(got), "/.local/bin") {
		t.Errorf("DefaultBinDir() = %q, want it to end in /.local/bin", got)
	}
}
