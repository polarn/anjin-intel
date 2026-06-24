package chatlog

import (
	"testing"
	"time"
	"unicode/utf16"
)

// encodeUTF16LE builds a UTF-16LE byte slice (with BOM) like the EVE client writes.
func encodeUTF16LE(s string) []byte {
	out := []byte{0xFF, 0xFE} // BOM
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

const sampleLog = `---------------------------------------------------------------
  Channel ID:      2_someid
  Channel Name:    Querious.imperium
  Listener:        Some Pilot
  Session started: 2026.06.23 19:00:00
---------------------------------------------------------------
[ 2026.06.23 19:04:11 ] Some Pilot > neut in FD-MLJ
[ 2026.06.23 19:05:02 ] Scout Alt > gate > gate camp at 1DQ1-A
`

func TestDecodeUTF16LE_RoundTrip(t *testing.T) {
	b := encodeUTF16LE(sampleLog)
	got := DecodeUTF16LE(b)
	if got != sampleLog {
		t.Fatalf("decode mismatch:\n got %q\nwant %q", got, sampleLog)
	}
}

func TestDecodeUTF16LE_OddTrailingByte(t *testing.T) {
	b := append(encodeUTF16LE("ab"), 0x41) // dangling half code unit
	if got := DecodeUTF16LE(b); got != "ab" {
		t.Errorf("odd trailing byte not dropped: got %q", got)
	}
}

func TestChannelName(t *testing.T) {
	if got := ChannelName(sampleLog); got != "Querious.imperium" {
		t.Errorf("ChannelName = %q, want Querious.imperium", got)
	}
	if got := ChannelName("no header here"); got != "" {
		t.Errorf("ChannelName on junk = %q, want empty", got)
	}
}

func TestChannelFromFilename(t *testing.T) {
	cases := map[string]string{
		"Querious.imperium_20260623_190000_91000001.txt": "Querious.imperium",
		"Local_20260623_190000_98000002.txt":             "Local",
		"notalog.txt":                                    "",
		"Local.txt":                                      "",
	}
	for in, want := range cases {
		if got := ChannelFromFilename(in); got != want {
			t.Errorf("ChannelFromFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		ok      bool
		sender  string
		message string
	}{
		{"message", "[ 2026.06.23 19:04:11 ] Some Pilot > neut in FD-MLJ", true, "Some Pilot", "neut in FD-MLJ"},
		{"message with arrow in body", "[ 2026.06.23 19:05:02 ] Scout Alt > gate > camp", true, "Scout Alt", "gate > camp"},
		{"leading BOM (EVE reopen quirk)", "\ufeff[ 2026.06.24 09:15:28 ] gatupojken > ddwqdqwd", true, "gatupojken", "ddwqdqwd"},
		{"crlf tolerated", "[ 2026.06.23 19:04:11 ] X > hi\r", true, "X", "hi"},
		{"header line", "  Channel Name:    Querious.imperium", false, "", ""},
		{"separator", "-----------------------------", false, "", ""},
		{"blank", "", false, "", ""},
		{"bad timestamp", "[ 2026.13.99 99:99:99 ] X > y", false, "", ""},
		{"channel MOTD (EVE System)", "[ 2026.06.24 11:19:27 ] EVE System > Channel MOTD: ESOTERIA // PARAGON SOUL", false, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, ok := ParseLine(tt.line)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			if e.Sender != tt.sender || e.Message != tt.message {
				t.Errorf("got sender=%q message=%q, want sender=%q message=%q", e.Sender, e.Message, tt.sender, tt.message)
			}
			if e.TS.Location() != time.UTC {
				t.Errorf("timestamp not UTC: %v", e.TS.Location())
			}
		})
	}
}
