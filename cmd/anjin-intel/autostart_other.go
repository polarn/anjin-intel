//go:build !linux && !windows

package main

// macOS would use a LaunchAgent (~/Library/LaunchAgents + launchctl bootstrap); until
// that exists the shipper is started by hand. install still writes the config, so
// `anjin-intel run` needs no flags.
type noAutostart struct{}

func newAutostarter() autostarter { return noAutostart{} }

func (noAutostart) Register(string) error { return errAutostartUnsupported }
func (noAutostart) Stop() error           { return nil }
func (noAutostart) Remove() error         { return nil }
func (noAutostart) State() string {
	return "unavailable on this OS — start it with `anjin-intel run`"
}
