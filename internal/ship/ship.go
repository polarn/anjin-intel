// Package ship POSTs batches of intel lines to the anjin server. The wire shape is
// the versioned contract in SPEC.md: {protocolVersion, lines:[{channel,ts,sender,
// message}]} with a Bearer enrollment token. stdlib only (net/http, encoding/json).
package ship

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/polarn/anjin-intel/internal/tail"
)

// ProtocolVersion is the wire contract version (must match the server's).
const ProtocolVersion = 1

// wireLine mirrors the server's expected JSON; ts is RFC3339 UTC.
type wireLine struct {
	Channel string `json:"channel"`
	TS      string `json:"ts"`
	Sender  string `json:"sender"`
	Message string `json:"message"`
}

type wireBatch struct {
	ProtocolVersion int        `json:"protocolVersion"`
	Lines           []wireLine `json:"lines"`
}

// ErrProtocol is returned when the server reports a version mismatch (HTTP 409) —
// the user should update the shipper. It stops the run loop (retrying won't help).
var ErrProtocol = errors.New("server rejected protocol version; update anjin-intel")

// Shipper posts batches to {server}/api/intel.
type Shipper struct {
	url    string
	token  string
	client *http.Client
}

// New builds a Shipper for the given server base URL and enrollment token.
func New(server, token string) *Shipper {
	return &Shipper{
		url:    strings.TrimRight(server, "/") + "/api/intel",
		token:  token,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Send POSTs one batch. It returns ErrProtocol on a 409 (caller should stop), nil
// on success, and a transient error otherwise (caller should retry/backoff).
func (s *Shipper) Send(ctx context.Context, lines []tail.Line) error {
	if len(lines) == 0 {
		return nil
	}
	batch := wireBatch{ProtocolVersion: ProtocolVersion, Lines: make([]wireLine, len(lines))}
	for i, l := range lines {
		batch.Lines[i] = wireLine{
			Channel: l.Channel,
			TS:      l.Entry.TS.UTC().Format(time.RFC3339),
			Sender:  l.Entry.Sender,
			Message: l.Entry.Message,
		}
	}
	body, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))

	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusConflict:
		return ErrProtocol
	default:
		return fmt.Errorf("server returned %s", resp.Status)
	}
}
