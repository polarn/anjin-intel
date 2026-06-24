// Command anjin-intel tails the EVE Online chat logs and ships intel lines to an
// anjin server. MVP: the `run` subcommand only (install/uninstall/status follow).
// Pure Go standard library — see github.com/polarn/anjin-intel.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/polarn/anjin-intel/internal/config"
	"github.com/polarn/anjin-intel/internal/ship"
	"github.com/polarn/anjin-intel/internal/tail"
)

// version is overwritten at release time via -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = runCmd(os.Args[2:])
	case "install":
		err = install(os.Args[2:])
	case "uninstall":
		err = uninstall(os.Args[2:])
	case "status":
		err = status(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println("anjin-intel", version)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("anjin-intel: %v", err)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `anjin-intel — ship EVE chat-log intel to your anjin server

Usage:
  anjin-intel install --server <url> --token <tok> [--logdir <path>] [--channels a,b]
  anjin-intel run     [--server <url> --token <tok> --logdir <path>]   (reads the install config if omitted)
  anjin-intel status
  anjin-intel uninstall

install registers a systemd user service that runs the shipper at login (Linux).
Channels are managed in the Intel tab; --channels is just an optional seed.
`)
}

// maxPending bounds the in-flight buffer if the server is unreachable; intel is
// ephemeral, so we drop the oldest rather than grow without limit.
const maxPending = 5000

func runCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	server := fs.String("server", "", "anjin server base URL (e.g. https://anjin.example.net)")
	token := fs.String("token", "", "enrollment token from the Intel tab")
	logdir := fs.String("logdir", "", "EVE Chatlogs directory to watch")
	channels := fs.String("channels", "", "comma-separated channel seed (the Intel tab is authoritative)")
	poll := fs.Duration("poll", 2*time.Second, "how often to scan the log directory")
	fs.Parse(args)

	// Fall back to the saved install config for any flag not given — so the systemd
	// unit can invoke `anjin-intel run` with no arguments.
	cfg, _ := config.Load()
	srv := firstNonEmpty(*server, cfg.Server)
	tok := firstNonEmpty(*token, cfg.Token)
	ld := firstNonEmpty(*logdir, cfg.Logdir)
	allow := splitList(*channels) // seed only — the server allowlist (Intel tab) is authoritative
	if len(allow) == 0 {
		allow = cfg.Channels
	}

	if srv == "" || tok == "" || ld == "" {
		return errors.New("need --server, --token and --logdir (or run `anjin-intel install` first)")
	}
	if _, err := os.Stat(ld); err != nil {
		return fmt.Errorf("logdir %q: %w", ld, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tl := tail.New(ld, allow)
	sh := ship.New(srv, tok)
	log.Printf("watching %s every %s; server=%s (channels managed in the Intel tab; seed=[%s])",
		ld, *poll, srv, strings.Join(allow, ","))

	return loop(ctx, tl, sh, *poll)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// syncInterval is how often the shipper reports the channels it has seen and refreshes
// the server-managed allowlist.
const syncInterval = 60 * time.Second

// configSync reports the seen channel names (for the Intel-tab picker) and pulls the
// server allowlist (authoritative when reachable; on error the local seed stands).
func configSync(ctx context.Context, tl *tail.Tailer, sh *ship.Shipper, announce bool) {
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if seen := tl.Seen(); len(seen) > 0 {
		if err := sh.ReportSeen(sctx, seen); err != nil {
			log.Printf("report seen channels: %v", err)
		}
	}
	chans, err := sh.Allowlist(sctx)
	if err != nil {
		if announce {
			log.Printf("server allowlist unavailable (%v) — using the --channels seed", err)
		}
		return
	}
	tl.SetAllowlist(chans)
	if announce {
		log.Printf("server allowlist: %d channel(s) [%s]", len(chans), strings.Join(chans, ", "))
	}
}

// loop polls the tailer, accumulates lines, and ships them with exponential backoff
// on transient failure. A protocol mismatch is fatal (retrying won't help). It also
// periodically syncs the server allowlist + reports seen channels.
func loop(ctx context.Context, tl *tail.Tailer, sh *ship.Shipper, poll time.Duration) error {
	configSync(ctx, tl, sh, true) // apply the server allowlist before the first poll
	lastSync := time.Now()

	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	var pending []tail.Line
	var fails int
	var nextAttempt time.Time
	var lastState time.Time // throttle heartbeat writes

	for {
		select {
		case <-ctx.Done():
			if len(pending) > 0 { // best-effort final flush
				fctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = sh.Send(fctx, pending)
				cancel()
			}
			log.Println("shutting down")
			return nil
		case <-ticker.C:
		}

		if time.Since(lastSync) >= syncInterval {
			configSync(ctx, tl, sh, false)
			lastSync = time.Now()
		}

		pending = appendCapped(pending, tl.Poll())
		if len(pending) == 0 || time.Now().Before(nextAttempt) {
			continue
		}
		err := sh.Send(ctx, pending)
		switch {
		case err == nil:
			pending, fails, nextAttempt = nil, 0, time.Time{}
			if now := time.Now(); now.Sub(lastState) > 30*time.Second { // heartbeat for `status`
				_ = config.SaveState(config.State{LastShip: now})
				lastState = now
			}
		case errors.Is(err, ship.ErrProtocol):
			return err
		case ctx.Err() != nil:
			return nil // shutting down; the next loop catches ctx.Done
		default:
			fails++
			d := backoff(fails)
			nextAttempt = time.Now().Add(d)
			log.Printf("ship failed (%v); %d line(s) buffered, retrying in %s", err, len(pending), d)
		}
	}
}

// appendCapped appends add to buf, dropping the oldest if it would exceed maxPending.
func appendCapped(buf, add []tail.Line) []tail.Line {
	buf = append(buf, add...)
	if len(buf) > maxPending {
		dropped := len(buf) - maxPending
		log.Printf("buffer full — dropping %d oldest line(s)", dropped)
		buf = buf[dropped:]
	}
	return buf
}

// backoff is exponential from 2s, capped at 2m.
func backoff(fails int) time.Duration {
	d := 2 * time.Second
	for i := 1; i < fails && d < 2*time.Minute; i++ {
		d *= 2
	}
	if d > 2*time.Minute {
		d = 2 * time.Minute
	}
	return d
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
