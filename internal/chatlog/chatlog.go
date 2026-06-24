// Package chatlog parses EVE Online chat-log files. They are UTF-16LE with a BOM:
// a header block (Channel ID / Channel Name / Listener / Session started) followed
// by message lines of the form
//
//	[ 2026.06.23 19:04:11 ] Sender Name > the message text
//
// Everything here is pure (bytes/strings in, values out) so it's unit-testable
// against fixtures. stdlib only — UTF-16 is decoded with unicode/utf16, not x/text.
package chatlog

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf16"
)

// Entry is one parsed chat message.
type Entry struct {
	TS      time.Time // UTC (EVE time)
	Sender  string
	Message string
}

// eveTimeLayout is the timestamp format inside the brackets; EVE time is UTC.
const eveTimeLayout = "2006.01.02 15:04:05"

var (
	lineRE    = regexp.MustCompile(`^\[ (\d{4}\.\d{2}\.\d{2} \d{2}:\d{2}:\d{2}) \] (.*?) > (.*)$`)
	channelRE = regexp.MustCompile(`(?m)^\s*Channel Name:\s*(.+?)\s*$`)
	fileRE    = regexp.MustCompile(`^(.+)_\d{8}_\d{6}_\d+\.txt$`)
)

// DecodeUTF16LE decodes a UTF-16LE byte slice (BOM stripped if present) to a string.
// A trailing odd byte (a half-written code unit) is dropped — callers that stream
// should hold it back and prepend it next time.
func DecodeUTF16LE(b []byte) string {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE { // UTF-16LE BOM
		b = b[2:]
	}
	n := len(b) / 2
	u := make([]uint16, n)
	for i := 0; i < n; i++ {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u))
}

// ChannelName extracts the channel name from a header block, or "" if not present.
func ChannelName(header string) string {
	if m := channelRE.FindStringSubmatch(header); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// ChannelFromFilename is a best-effort fallback when the header is unreadable: EVE
// names logs "<channel>_<YYYYMMDD>_<HHMMSS>_<listenerID>.txt". Returns "" if the
// name doesn't match that shape.
func ChannelFromFilename(name string) string {
	if m := fileRE.FindStringSubmatch(name); m != nil {
		return m[1]
	}
	return ""
}

// ParseLine parses one chat line. ok is false for anything that isn't a message
// line (header lines, separators, blanks) — so callers can feed every line through
// it and only message lines come out. A trailing CR (CRLF logs) is tolerated.
func ParseLine(line string) (Entry, bool) {
	line = strings.TrimRight(line, "\r")
	m := lineRE.FindStringSubmatch(line)
	if m == nil {
		return Entry{}, false
	}
	ts, err := time.ParseInLocation(eveTimeLayout, m[1], time.UTC)
	if err != nil {
		return Entry{}, false
	}
	return Entry{TS: ts, Sender: strings.TrimSpace(m[2]), Message: m[3]}, true
}
