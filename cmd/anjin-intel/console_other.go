//go:build !windows

package main

// detachConsole is Windows-only: no other platform hands a background process a console
// window it has to get rid of.
func detachConsole() {}
