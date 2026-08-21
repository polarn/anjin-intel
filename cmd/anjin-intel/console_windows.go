//go:build windows

package main

import "syscall"

// detachConsole releases the console the Task Scheduler gives a console-subsystem
// process at logon, so the shipper doesn't leave a black window on screen for the rest
// of the session. There is a brief flash as it starts, which is the accepted trade:
// building the whole binary with -H windowsgui would kill the window for good but also
// silence `status` and `run` in a terminal, and one binary has to serve both.
func detachConsole() {
	_, _, _ = syscall.NewLazyDLL("kernel32.dll").NewProc("FreeConsole").Call()
}
