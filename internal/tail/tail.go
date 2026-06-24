// Package tail watches an EVE Chatlogs directory and emits new message lines from
// allowlisted channels. It polls (stat for growth, read the tail) to stay
// stdlib-only — no fsnotify. One file per channel/session; new session files
// appear over time, so the directory itself is rescanned each poll.
//
// No backfill: files that already exist when the tailer starts are skipped to EOF
// (we only read their header to learn the channel). A file that appears later is a
// new session opened after we started, so it's read from the beginning — its header
// lines aren't messages, so the parser drops them and only real chat is emitted.
package tail

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/polarn/anjin-intel/internal/chatlog"
)

// Line is a parsed message tagged with its channel.
type Line struct {
	Channel string
	Entry   chatlog.Entry
}

// file is the per-file tailing state.
type file struct {
	channel string
	allowed bool   // channel is on the allowlist (skip reading otherwise)
	offset  int64  // bytes consumed from disk
	odd     []byte // a trailing half code-unit held until its other byte arrives
	pending string // decoded text not yet terminated by a newline
}

// Tailer tracks per-file state across polls. Not safe for concurrent use — Poll,
// SetAllowlist and Seen must be called from one goroutine (the run loop).
type Tailer struct {
	dir     string
	allow   map[string]bool
	files   map[string]*file
	seen    map[string]bool // every channel name observed in the dir (for discovery)
	started bool            // false until the first Poll registers the startup set
}

// New builds a Tailer for dir, seeded with the channels in allow (the server
// allowlist later overrides this via SetAllowlist).
func New(dir string, allow []string) *Tailer {
	return &Tailer{dir: dir, allow: toSet(allow), files: map[string]*file{}, seen: map[string]bool{}}
}

func toSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, c := range items {
		if c = strings.TrimSpace(c); c != "" {
			m[c] = true
		}
	}
	return m
}

// SetAllowlist replaces the allowlist (e.g. from the server's /api/intel/config) and
// re-evaluates every tracked file. A channel that's newly enabled skips to the file's
// current end, so ticking it ships from now on — not a replay of everything that
// arrived while it was disabled.
func (t *Tailer) SetAllowlist(channels []string) {
	next := toSet(channels)
	for name, f := range t.files {
		nowAllowed := next[f.channel]
		if nowAllowed && !f.allowed {
			if info, err := os.Stat(filepath.Join(t.dir, name)); err == nil {
				f.offset, f.odd, f.pending = info.Size(), nil, ""
			}
		}
		f.allowed = nowAllowed
	}
	t.allow = next
}

// Seen returns every channel name observed in the log dir (names only), sorted — for
// the server's seen-list so the SPA can offer a channel picker.
func (t *Tailer) Seen() []string {
	out := make([]string, 0, len(t.seen))
	for c := range t.seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// headerProbe is how many bytes we read from a file's start to find the channel name.
const headerProbe = 8 << 10

// Poll scans the directory once and returns any new allowlisted lines. Errors on
// individual files are skipped (best-effort); a missing directory yields no lines.
func (t *Tailer) Poll() []Line {
	entries, err := os.ReadDir(t.dir)
	if err != nil {
		return nil
	}
	startup := !t.started
	t.started = true

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".txt") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // stable order across polls

	var out []Line
	for _, name := range names {
		f := t.files[name]
		if f == nil {
			f = t.register(filepath.Join(t.dir, name), name, startup)
			t.files[name] = f
		}
		if f.allowed {
			out = append(out, t.read(filepath.Join(t.dir, name), f)...)
		}
	}
	return out
}

// register resolves a file's channel and decides where tailing begins: EOF for a
// file present at startup (no backfill), the start for one that appears later.
func (t *Tailer) register(path, name string, startup bool) *file {
	f := &file{}
	info, err := os.Stat(path)
	if err != nil {
		return f // unreadable; allowed stays false
	}
	f.channel = t.resolveChannel(path, name)
	if f.channel != "" {
		t.seen[f.channel] = true // record for discovery, even if not allowlisted
	}
	f.allowed = t.allow[f.channel]
	if startup {
		f.offset = info.Size() // skip existing history
	}
	return f
}

// resolveChannel reads the header region for the Channel Name, falling back to the
// filename shape. Uses the BOM-stripping decoder (this read is from the file start).
func (t *Tailer) resolveChannel(path, name string) string {
	fh, err := os.Open(path)
	if err != nil {
		return chatlog.ChannelFromFilename(name)
	}
	defer fh.Close()
	buf := make([]byte, headerProbe)
	n, _ := fh.Read(buf)
	if ch := chatlog.ChannelName(chatlog.DecodeUTF16LE(buf[:n])); ch != "" {
		return ch
	}
	return chatlog.ChannelFromFilename(name)
}

// read consumes new bytes from path and emits any complete message lines.
func (t *Tailer) read(path string, f *file) []Line {
	fh, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer fh.Close()
	info, err := fh.Stat()
	if err != nil || info.Size() <= f.offset {
		return nil // nothing new (truncation/rotation is left alone)
	}
	if _, err := fh.Seek(f.offset, 0); err != nil {
		return nil
	}
	chunk := make([]byte, info.Size()-f.offset)
	n, _ := fh.Read(chunk)
	f.offset += int64(n)

	// Re-attach a half code-unit held from last poll, then peel any new odd byte so
	// we only decode whole UTF-16 code units.
	raw := append(f.odd, chunk[:n]...)
	if len(raw)%2 == 1 {
		f.odd = []byte{raw[len(raw)-1]}
		raw = raw[:len(raw)-1]
	} else {
		f.odd = nil
	}
	f.pending += decodeUTF16LE(raw)

	var out []Line
	for {
		i := strings.IndexByte(f.pending, '\n')
		if i < 0 {
			break
		}
		line := f.pending[:i]
		f.pending = f.pending[i+1:]
		if e, ok := chatlog.ParseLine(line); ok {
			out = append(out, Line{Channel: f.channel, Entry: e})
		}
	}
	return out
}

// decodeUTF16LE decodes a UTF-16LE byte slice WITHOUT stripping a BOM — it's for
// stream continuations, which carry no BOM (and a legitimate U+FEFF in a message
// must survive). A BOM at a new file's offset 0 decodes to a U+FEFF on the first
// header line, which the parser drops anyway. Callers pass an even-length slice.
func decodeUTF16LE(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u))
}
