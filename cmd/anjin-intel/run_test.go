package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/polarn/anjin-intel/internal/ship"
	"github.com/polarn/anjin-intel/internal/tail"
)

func u16le(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}

// End-to-end: a line appended after startup flows through tail → ship → server,
// while pre-existing backlog is not shipped.
func TestLoop_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Local_20260623_190000_1.txt")
	head := "  Channel Name:    Local\n"
	old := "[ 2026.06.23 19:00:00 ] Pilot > OLD backlog line\n"
	if err := os.WriteFile(path, append([]byte{0xFF, 0xFE}, u16le(head+old)...), 0o644); err != nil {
		t.Fatal(err)
	}

	type batch struct {
		Lines []struct{ Message string } `json:"lines"`
	}
	got := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var b batch
		_ = json.Unmarshal(body, &b)
		for _, l := range b.Lines {
			got <- l.Message
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tl := tail.New(dir, []string{"Local"})
	tl.Poll() // register the existing file at EOF — backlog must not ship

	// Append a fresh line after startup registration.
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	fh.Write(u16le("[ 2026.06.23 19:10:00 ] Pilot > FRESH after start\n"))
	fh.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { loop(ctx, tl, ship.New(srv.URL, "tok"), 10*time.Millisecond); close(done) }()

	select {
	case msg := <-got:
		if msg != "FRESH after start" {
			t.Fatalf("shipped wrong line: %q (backlog leaked?)", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the fresh line to ship")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not stop on ctx cancel")
	}
}
