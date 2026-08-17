package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Chatlogs-directory discovery, shared by the per-OS detectors in detect_linux.go
// and detect_windows.go. Everything here is path/filesystem logic with no OS-specific
// API, so it's unit-testable on any platform.

// isChatlogsDir reports whether path looks like an EVE chat-log directory: a dir
// named "Chatlogs" whose parent ends in EVE/logs. ToSlash keeps the suffix test
// working on Windows, where the separator is a backslash.
func isChatlogsDir(path string) bool {
	return filepath.Base(path) == "Chatlogs" &&
		strings.HasSuffix(filepath.ToSlash(filepath.Dir(path)), "EVE/logs")
}

// chatlogsUnder maps document roots to the Chatlogs dir EVE would use under each
// (<root>/EVE/logs/Chatlogs). Empty roots are dropped.
func chatlogsUnder(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		if r != "" {
			out = append(out, filepath.Join(r, "EVE", "logs", "Chatlogs"))
		}
	}
	return out
}

// newestLog returns the mtime of the most recently written .txt in dir. Ranking
// candidates by their newest LOG rather than the directory's own mtime matters: a
// long-dead archive can have its directory touched by a sync client (OneDrive)
// ages after EVE last wrote a line to it, which would make a stale dir look live.
func newestLog(dir string) (time.Time, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}, false
	}
	var newest time.Time
	var found bool
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if !found || info.ModTime().After(newest) {
			newest, found = info.ModTime(), true
		}
	}
	return newest, found
}

// pickChatlogs chooses the live Chatlogs dir from candidates: the one holding the
// most recently written log. Candidates that don't exist are ignored, and ones
// resolving to the same real directory are collapsed — on Windows the legacy
// "My Documents" junction points at "Documents", so without this a duplicate could
// outrank (or merely shadow) its own target. A candidate with no logs at all is
// kept only as a last resort, so an empty-but-present dir never beats a live one.
func pickChatlogs(candidates []string) string {
	var best string
	var bestMod time.Time
	var fallback string
	seen := make(map[string]bool, len(candidates))

	for _, c := range candidates {
		if c == "" {
			continue
		}
		key := c
		if real, err := filepath.EvalSymlinks(c); err == nil {
			key = real
		}
		if seen[key] {
			continue
		}
		seen[key] = true

		if info, err := os.Stat(c); err != nil || !info.IsDir() {
			continue
		}
		mod, ok := newestLog(c)
		if !ok {
			if fallback == "" {
				fallback = c
			}
			continue
		}
		if best == "" || mod.After(bestMod) {
			best, bestMod = c, mod
		}
	}
	if best != "" {
		return best
	}
	return fallback
}

// walkChatlogs collects every Chatlogs dir under the given roots, descending at
// most maxDepth levels below each root and never into a directory named in skip.
// It's the fallback for layouts the explicit candidates don't cover (wrapper
// prefixes on Linux, an unguessable localized folder name on Windows).
func walkChatlogs(roots []string, maxDepth int, skip map[string]bool) []string {
	var found []string
	for _, root := range roots {
		if root == "" {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil // best-effort: unreadable subtrees are skipped
			}
			if path != root && skip[d.Name()] {
				return fs.SkipDir
			}
			if rel, e := filepath.Rel(root, path); e == nil && strings.Count(rel, string(os.PathSeparator)) > maxDepth {
				return fs.SkipDir
			}
			if isChatlogsDir(path) {
				found = append(found, path)
				return fs.SkipDir // don't descend into the logs themselves
			}
			return nil
		})
	}
	return found
}
