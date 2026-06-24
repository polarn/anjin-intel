package ship

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/polarn/anjin-intel/internal/chatlog"
	"github.com/polarn/anjin-intel/internal/tail"
)

func sampleLines() []tail.Line {
	ts := time.Date(2026, 6, 23, 19, 4, 11, 0, time.UTC)
	return []tail.Line{
		{Channel: "Querious.imperium", Entry: chatlog.Entry{TS: ts, Sender: "Scout", Message: "neut in FD-MLJ"}},
	}
}

func TestSend_SuccessShape(t *testing.T) {
	var gotAuth, gotCT string
	var gotBatch wireBatch
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/intel" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBatch)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := New(srv.URL, "tok123").Send(context.Background(), sampleLines()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	if gotBatch.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocolVersion = %d, want %d", gotBatch.ProtocolVersion, ProtocolVersion)
	}
	if len(gotBatch.Lines) != 1 || gotBatch.Lines[0].Message != "neut in FD-MLJ" {
		t.Fatalf("lines round-trip wrong: %+v", gotBatch.Lines)
	}
	if gotBatch.Lines[0].TS != "2026-06-23T19:04:11Z" {
		t.Errorf("ts = %q, want RFC3339 UTC", gotBatch.Lines[0].TS)
	}
}

func TestSend_ProtocolMismatchIsFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()
	err := New(srv.URL, "t").Send(context.Background(), sampleLines())
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("want ErrProtocol, got %v", err)
	}
}

func TestSend_TransientError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	err := New(srv.URL, "t").Send(context.Background(), sampleLines())
	if err == nil || errors.Is(err, ErrProtocol) {
		t.Fatalf("want transient error, got %v", err)
	}
}

func TestSend_EmptyIsNoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not POST for an empty batch")
	}))
	defer srv.Close()
	if err := New(srv.URL, "t").Send(context.Background(), nil); err != nil {
		t.Fatalf("empty Send: %v", err)
	}
}
