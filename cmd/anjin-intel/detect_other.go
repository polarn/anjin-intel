//go:build !linux && !windows

package main

// detectLogdir has no default for this OS yet (macOS keeps its Chatlogs under a
// Wine/CrossOver wrapper, whose layout varies) — pass --logdir explicitly.
func detectLogdir() string { return "" }
