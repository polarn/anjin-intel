//go:build linux

package main

import "os"

// detectSkip are big/irrelevant directories we never descend into when searching for
// the Chatlogs dir (Steam dirs are intentionally NOT here — Proton prefixes live there).
var detectSkip = map[string]bool{
	".cache": true, ".cargo": true, ".rustup": true, ".npm": true, ".gradle": true,
	".m2": true, "node_modules": true, ".git": true, "go": true, ".venv": true,
	"__pycache__": true, ".mozilla": true, ".thunderbird": true,
}

// detectDepth caps how far below $HOME we look. Wrapper prefixes nest deeply
// (e.g. ~/Faugus/eve-online/drive_c/users/steamuser/Documents/EVE/logs/Chatlogs).
const detectDepth = 12

// detectLogdir best-effort finds the EVE Chatlogs dir under $HOME, across any layout
// (native, Steam/Proton, Lutris, Faugus, Bottles…) by a bounded walk — globs can't
// match the variable wrapper depth. Prefers the install that most recently wrote a log.
func detectLogdir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return pickChatlogs(walkChatlogs([]string{home}, detectDepth, detectSkip))
}
