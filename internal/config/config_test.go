package config

import (
	"os"
	"testing"
	"time"
)

func TestConfigRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
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
	// The token is sensitive — the file must be 0600.
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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
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
