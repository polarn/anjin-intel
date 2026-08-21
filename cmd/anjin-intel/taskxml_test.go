package main

import (
	"strings"
	"testing"
	"unicode/utf16"
)

// TestTaskXMLDefeatsHostileDefaults pins the four Task Scheduler defaults that would
// each silently stop a long-running shipper. They are asserted rather than trusted
// because every one of them fails LATE and QUIETLY — the execution-time limit kills the
// task three days after a successful install, by which point nothing points back here.
func TestTaskXMLDefeatsHostileDefaults(t *testing.T) {
	doc, err := renderTaskXML(`C:\Users\x\AppData\Local\Programs\anjin\anjin-intel.exe`, `PC\user`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>",                  // default PT72H kills it
		"<DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>", // default true
		"<StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>",         // default true
		"<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("task XML missing %s", want)
		}
	}
}

func TestTaskXMLRunsUnelevatedAsTheUser(t *testing.T) {
	doc, err := renderTaskXML(`C:\bin\anjin-intel.exe`, `PC\user`)
	if err != nil {
		t.Fatal(err)
	}
	// Registration is elevated; the task itself must NOT be, or it would run against
	// the wrong profile and never find the user's Chatlogs.
	if !strings.Contains(doc, "<RunLevel>LeastPrivilege</RunLevel>") {
		t.Error("task must run unelevated (LeastPrivilege)")
	}
	if !strings.Contains(doc, "<LogonType>InteractiveToken</LogonType>") {
		t.Error("task must use InteractiveToken")
	}
	// Owner pinned explicitly, since registration happens via an elevated process.
	if strings.Count(doc, `<UserId>PC\user</UserId>`) != 2 {
		t.Errorf("expected UserId on both principal and trigger, got:\n%s", doc)
	}
	if !strings.Contains(doc, "<Arguments>run --background</Arguments>") {
		t.Error("task must launch with --background so the console window is released")
	}
}

func TestTaskXMLQuotesAndEscapesThePath(t *testing.T) {
	doc, err := renderTaskXML(`C:\Program Files\a & b\anjin-intel.exe`, `PC\user`)
	if err != nil {
		t.Fatal(err)
	}
	// A space in the path is ordinary on Windows ("Program Files"), and an ampersand
	// is legal in a username — unescaped it would make the document malformed.
	if !strings.Contains(doc, `<Command>"C:\Program Files\a &amp; b\anjin-intel.exe"</Command>`) {
		t.Errorf("path not quoted+escaped, got:\n%s", doc)
	}
}

// TestUTF16LEWithBOM covers the encoding schtasks demands: a declaration saying
// encoding="UTF-16" must be backed by real UTF-16LE bytes WITH a BOM, or the file is
// rejected as "(1,2)::ERROR: one root element" — an error that says nothing about
// encoding.
func TestUTF16LEWithBOM(t *testing.T) {
	b := utf16LEWithBOM("<Task/>")
	if len(b) < 2 || b[0] != 0xFF || b[1] != 0xFE {
		t.Fatalf("missing UTF-16LE BOM, got % x", b[:min(4, len(b))])
	}
	if len(b)%2 != 0 {
		t.Fatalf("UTF-16 output must be an even number of bytes, got %d", len(b))
	}
	// Round-trip everything after the BOM.
	u := make([]uint16, 0, len(b)/2-1)
	for i := 2; i < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	if got := string(utf16.Decode(u)); got != "<Task/>" {
		t.Errorf("round-trip = %q, want %q", got, "<Task/>")
	}
}

func TestUTF16LEWithBOMHandlesNonASCII(t *testing.T) {
	// The install path can contain non-ASCII (a Swedish user folder, say), which is the
	// whole reason the file is UTF-16 rather than bytes.
	const in = `C:\Users\Björn\Dokument`
	b := utf16LEWithBOM(in)
	u := make([]uint16, 0, len(b)/2-1)
	for i := 2; i < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	if got := string(utf16.Decode(u)); got != in {
		t.Errorf("round-trip = %q, want %q", got, in)
	}
}
