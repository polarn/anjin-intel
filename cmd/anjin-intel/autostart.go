package main

import "errors"

// Autostart backends. Each OS registers a login-time launcher for the shipper in its
// own idiom — a systemd user unit on Linux, a Task Scheduler logon task on Windows —
// behind this one interface, so install/uninstall/status stay OS-agnostic.
//
// The seam mirrors detect_<goos>.go: one file per OS, selected by build tag, over a
// shared caller. Adding macOS (a LaunchAgent) is one more file, not a rewrite.

// errAutostartUnsupported means this OS has no backend yet. It is NOT a failure:
// install still saved the config, so callers report it and carry on.
var errAutostartUnsupported = errors.New("no autostart backend on this OS")

// errElevationDeclined means the user dismissed the UAC prompt. Registration didn't
// happen, but everything before it did — callers must say so rather than claim success.
var errElevationDeclined = errors.New("elevation declined")

type autostarter interface {
	// Register creates or replaces the login entry for bin and starts it now.
	Register(bin string) error
	// Stop halts a running instance so its .exe can be replaced. Best-effort:
	// required on Windows, which locks a running image, and a no-op on Linux.
	Stop() error
	// Remove disables and deletes the login entry. Best-effort.
	Remove() error
	// State is a one-line description for `status`.
	State() string
}
