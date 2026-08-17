package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mkChatlogs creates <root>/EVE/logs/Chatlogs and returns it. Each entry of logs is
// a "name:age" pair written as a log file with that mtime, so tests can express
// "this install was last written to N ago".
func mkChatlogs(t *testing.T, root string, logs ...string) string {
	t.Helper()
	dir := filepath.Join(root, "EVE", "logs", "Chatlogs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, spec := range logs {
		name, ageStr, ok := strings.Cut(spec, ":")
		if !ok {
			t.Fatalf("bad log spec %q", spec)
		}
		age, err := time.ParseDuration(ageStr)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		when := time.Now().Add(-age)
		if err := os.Chtimes(p, when, when); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestIsChatlogsDir(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{filepath.Join("home", "me", "EVE", "logs", "Chatlogs"), true},
		{filepath.Join("c:", "Users", "me", "Documents", "EVE", "logs", "Chatlogs"), true},
		{filepath.Join("home", "me", "EVE", "logs", "Gamelogs"), false},
		{filepath.Join("home", "me", "EVE", "Chatlogs"), false},
		{filepath.Join("home", "me", "logs", "Chatlogs"), false},
	} {
		if got := isChatlogsDir(tc.path); got != tc.want {
			t.Errorf("isChatlogsDir(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestChatlogsUnder(t *testing.T) {
	got := chatlogsUnder([]string{"/docs", "", "/other"})
	want := []string{
		filepath.Join("/docs", "EVE", "logs", "Chatlogs"),
		filepath.Join("/other", "EVE", "logs", "Chatlogs"),
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// The live install must win over a stale archive even when the archive holds far
// more logs — this is the OneDrive case: an old redirected copy left behind years
// ago sits next to the directory EVE actually writes to today.
func TestPickChatlogs_NewestLogWins(t *testing.T) {
	base := t.TempDir()
	live := mkChatlogs(t, filepath.Join(base, "Documents"), "east.imperium.txt:5m")
	stale := mkChatlogs(t, filepath.Join(base, "OneDrive", "Dokument"),
		"old1.txt:8760h", "old2.txt:8760h", "old3.txt:8760h")

	if got := pickChatlogs([]string{stale, live}); got != live {
		t.Errorf("pickChatlogs = %q, want the live dir %q", got, live)
	}
	// Order must not matter.
	if got := pickChatlogs([]string{live, stale}); got != live {
		t.Errorf("reversed: pickChatlogs = %q, want %q", got, live)
	}
}

// A sync client touching a dead archive's *directory* must not make it look live —
// that's why ranking uses the newest log inside, not the directory's own mtime.
func TestPickChatlogs_IgnoresDirectoryMtime(t *testing.T) {
	base := t.TempDir()
	live := mkChatlogs(t, filepath.Join(base, "Documents"), "current.txt:1m")
	stale := mkChatlogs(t, filepath.Join(base, "OneDrive"), "ancient.txt:8760h")

	now := time.Now()
	if err := os.Chtimes(stale, now, now); err != nil { // as if OneDrive just synced it
		t.Fatal(err)
	}
	if got := pickChatlogs([]string{stale, live}); got != live {
		t.Errorf("pickChatlogs = %q, want %q (dir mtime must not outrank log mtime)", got, live)
	}
}

// Windows keeps a legacy "My Documents" junction pointing at "Documents"; the same
// directory reached by two paths must be considered once.
func TestPickChatlogs_DedupesLinkedDuplicates(t *testing.T) {
	base := t.TempDir()
	real := mkChatlogs(t, filepath.Join(base, "Documents"), "a.txt:1m")
	link := filepath.Join(base, "My Documents")
	if err := os.Symlink(filepath.Join(base, "Documents"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	viaLink := filepath.Join(link, "EVE", "logs", "Chatlogs")

	got := pickChatlogs([]string{viaLink, real})
	if got != viaLink {
		t.Errorf("pickChatlogs = %q, want the first-listed path %q", got, viaLink)
	}
}

func TestPickChatlogs_MissingAndEmpty(t *testing.T) {
	base := t.TempDir()
	missing := filepath.Join(base, "nope", "EVE", "logs", "Chatlogs")
	empty := mkChatlogs(t, filepath.Join(base, "Empty"))

	if got := pickChatlogs([]string{missing}); got != "" {
		t.Errorf("pickChatlogs(missing) = %q, want \"\"", got)
	}
	// An existing-but-empty dir is a last resort, better than nothing.
	if got := pickChatlogs([]string{missing, empty}); got != empty {
		t.Errorf("pickChatlogs = %q, want the empty-but-present dir %q", got, empty)
	}
	// ...but it must never beat a dir with actual logs.
	live := mkChatlogs(t, filepath.Join(base, "Live"), "a.txt:1h")
	if got := pickChatlogs([]string{empty, live}); got != live {
		t.Errorf("pickChatlogs = %q, want %q", got, live)
	}
	if got := pickChatlogs(nil); got != "" {
		t.Errorf("pickChatlogs(nil) = %q, want \"\"", got)
	}
}

// The fallback walk is what finds a localized documents folder ("Dokument") that no
// hardcoded English path would match.
func TestWalkChatlogs_FindsLocalizedFolder(t *testing.T) {
	base := t.TempDir()
	want := mkChatlogs(t, filepath.Join(base, "OneDrive", "Dokument"), "a.txt:1m")

	got := walkChatlogs([]string{base}, 5, nil)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("walkChatlogs = %v, want [%s]", got, want)
	}
}

func TestWalkChatlogs_DepthCapAndSkip(t *testing.T) {
	base := t.TempDir()
	deep := mkChatlogs(t, filepath.Join(base, "a", "b", "c", "d"), "x.txt:1m")
	if got := walkChatlogs([]string{base}, 2, nil); len(got) != 0 {
		t.Errorf("walkChatlogs with depth 2 found %v, want nothing (%s is deeper)", got, deep)
	}
	if got := walkChatlogs([]string{base}, 12, nil); len(got) != 1 {
		t.Errorf("walkChatlogs with depth 12 found %v, want the deep dir", got)
	}

	skipped := t.TempDir()
	mkChatlogs(t, filepath.Join(skipped, "AppData"), "x.txt:1m")
	if got := walkChatlogs([]string{skipped}, 12, map[string]bool{"AppData": true}); len(got) != 0 {
		t.Errorf("walkChatlogs found %v inside a skipped dir", got)
	}
}

func TestWalkChatlogs_MissingRootIsHarmless(t *testing.T) {
	if got := walkChatlogs([]string{filepath.Join(t.TempDir(), "nope"), ""}, 5, nil); len(got) != 0 {
		t.Errorf("walkChatlogs = %v, want nothing", got)
	}
}
