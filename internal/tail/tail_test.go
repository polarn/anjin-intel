package tail

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func u16le(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}

func withBOM(b []byte) []byte { return append([]byte{0xFF, 0xFE}, b...) }

func header(channel string) string {
	return "---------------------------------\n" +
		"  Channel ID:      x\n" +
		"  Channel Name:    " + channel + "\n" +
		"  Listener:        Me\n" +
		"  Session started: 2026.06.23 19:00:00\n" +
		"---------------------------------\n"
}

func msg(body string) string { return "[ 2026.06.23 19:04:11 ] Pilot > " + body + "\n" }

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, withBOM(u16le(content)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendBytes(t *testing.T, path string, b []byte) {
	t.Helper()
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	if _, err := fh.Write(b); err != nil {
		t.Fatal(err)
	}
}

func bodies(lines []Line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Entry.Message
	}
	return out
}

// A file present at startup is skipped to EOF (no backfill); only lines appended
// after the first Poll are emitted.
func TestTailer_NoBacklogThenAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Local_20260623_190000_1.txt")
	writeFile(t, path, header("Local")+msg("old line before start"))

	tl := New(dir, []string{"Local"})
	if got := tl.Poll(); len(got) != 0 {
		t.Fatalf("startup should backfill nothing, got %v", bodies(got))
	}
	appendBytes(t, path, u16le(msg("fresh line")))
	got := tl.Poll()
	if len(got) != 1 || got[0].Entry.Message != "fresh line" || got[0].Channel != "Local" {
		t.Fatalf("want one 'fresh line' on Local, got %v", got)
	}
}

// A file that appears after startup is a new session: read from the start, header
// lines dropped, all messages emitted.
func TestTailer_NewFileReadFromStart(t *testing.T) {
	dir := t.TempDir()
	tl := New(dir, []string{"Local"})
	tl.Poll() // mark started on an empty dir

	path := filepath.Join(dir, "Local_20260623_193000_2.txt")
	writeFile(t, path, header("Local")+msg("one")+msg("two"))
	got := tl.Poll()
	if want := []string{"one", "two"}; strings.Join(bodies(got), ",") != strings.Join(want, ",") {
		t.Fatalf("want %v, got %v", want, bodies(got))
	}
}

// Channels not on the allowlist are never emitted.
func TestTailer_AllowlistFilter(t *testing.T) {
	dir := t.TempDir()
	tl := New(dir, []string{"Local"})
	tl.Poll()

	writeFile(t, filepath.Join(dir, "Corp_20260623_193000_3.txt"), header("Corp")+msg("secret corp chatter"))
	if got := tl.Poll(); len(got) != 0 {
		t.Fatalf("un-allowlisted channel leaked: %v", bodies(got))
	}
}

// Seen tracks every channel observed (even un-allowlisted), and SetAllowlist switches
// which ship — a newly-enabled channel starts from "now", not the disabled backlog.
func TestTailer_SeenAndSetAllowlist(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "Local_20260623_190000_1.txt")
	corp := filepath.Join(dir, "Corp_20260623_190000_2.txt")
	writeFile(t, local, header("Local")+msg("old"))
	writeFile(t, corp, header("Corp")+msg("old"))

	tl := New(dir, []string{"Local"})
	tl.Poll() // register both at EOF (no backfill); both observed

	if got := strings.Join(tl.Seen(), ","); got != "Corp,Local" {
		t.Fatalf("Seen() = %q, want Corp,Local", got)
	}

	// Local is allowlisted, Corp isn't.
	appendBytes(t, local, u16le(msg("local 1")))
	appendBytes(t, corp, u16le(msg("corp 1")))
	if got := tl.Poll(); len(got) != 1 || got[0].Channel != "Local" {
		t.Fatalf("want Local only, got %v", bodies(got))
	}

	// Switch the allowlist to Corp. The Corp backlog ("corp 1") must NOT replay.
	tl.SetAllowlist([]string{"Corp"})
	appendBytes(t, corp, u16le(msg("corp 2")))
	appendBytes(t, local, u16le(msg("local 2")))
	got := tl.Poll()
	if len(got) != 1 || got[0].Channel != "Corp" || got[0].Entry.Message != "corp 2" {
		t.Fatalf("after SetAllowlist want only 'corp 2' on Corp, got %v", got)
	}
}

// A line split across polls — including at an odd byte boundary (a half UTF-16 code
// unit) — is buffered and emitted once complete.
func TestTailer_PartialLineAndOddByteCarry(t *testing.T) {
	dir := t.TempDir()
	tl := New(dir, []string{"Local"})
	tl.Poll()

	path := filepath.Join(dir, "Local_20260623_194000_4.txt")
	writeFile(t, path, header("Local")) // header only, no messages yet
	if got := tl.Poll(); len(got) != 0 {
		t.Fatalf("header-only should emit nothing, got %v", bodies(got))
	}

	full := u16le(msg("split message")) // even length, ends with 0x0A 0x00
	// First append everything but the final byte → an ODD-length new chunk with no
	// terminating newline yet.
	appendBytes(t, path, full[:len(full)-1])
	if got := tl.Poll(); len(got) != 0 {
		t.Fatalf("incomplete line should emit nothing, got %v", bodies(got))
	}
	// Append the held-back final byte → completes the code unit AND the newline.
	appendBytes(t, path, full[len(full)-1:])
	got := tl.Poll()
	if len(got) != 1 || got[0].Entry.Message != "split message" {
		t.Fatalf("want 'split message' after odd-byte carry, got %v", got)
	}
}
