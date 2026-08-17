//go:build windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	modshell32 = syscall.NewLazyDLL("shell32.dll")
	modole32   = syscall.NewLazyDLL("ole32.dll")

	procSHGetKnownFolderPath = modshell32.NewProc("SHGetKnownFolderPath")
	procCoTaskMemFree        = modole32.NewProc("CoTaskMemFree")
)

// folderIDDocuments is FOLDERID_Documents, {FDD39AD0-238F-46AF-ADB4-6C85480369C7}.
var folderIDDocuments = syscall.GUID{
	Data1: 0xFDD39AD0,
	Data2: 0x238F,
	Data3: 0x46AF,
	Data4: [8]byte{0xAD, 0xB4, 0x6C, 0x85, 0x48, 0x03, 0x69, 0xC7},
}

// documentsDir asks Windows where "Documents" actually is, via SHGetKnownFolderPath.
// This is the only generally-correct answer, because the folder is BOTH localized
// (it's "Dokument" on a Swedish install, "Documents" on an English one) AND
// redirectable (OneDrive relocates it under %USERPROFILE%\OneDrive, which is the
// default on Windows 11). No hardcoded English path covers both. Returns "" if the
// call fails; detectLogdir then falls back to guessed paths and a bounded walk.
func documentsDir() string {
	var p *uint16
	r, _, _ := procSHGetKnownFolderPath.Call(
		uintptr(unsafe.Pointer(&folderIDDocuments)),
		0, // no flags: the current, possibly redirected, path
		0, // the calling user
		uintptr(unsafe.Pointer(&p)),
	)
	if r != 0 || p == nil {
		return ""
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(p))) // the caller owns the buffer
	return utf16PtrToString(p)
}

// utf16PtrToString copies a NUL-terminated UTF-16 string allocated by Windows.
func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for ptr := unsafe.Pointer(p); *(*uint16)(ptr) != 0; n++ {
		ptr = unsafe.Add(ptr, unsafe.Sizeof(uint16(0)))
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}

// detectWalkDepth bounds the fallback walk. It must reach <root>/<docs>/EVE/logs/
// Chatlogs (3 levels below an OneDrive root) with a little slack.
const detectWalkDepth = 5

// detectSkip keeps the fallback walk out of the noisy subtrees. AppData is the big
// one: enormous, full of junctions and access-denied dirs, and never holds chat logs.
var detectSkip = map[string]bool{
	"AppData": true, "Application Data": true, "node_modules": true, ".git": true,
}

// detectLogdir best-effort finds the EVE Chatlogs directory. EVE's Windows layout is
// fixed (<Documents>\EVE\logs\Chatlogs), so we probe known locations first and only
// fall back to a shallow walk — deliberately NOT the deep $HOME walk the Linux build
// uses, which would drag through AppData.
func detectLogdir() string {
	userProfile, oneDrive := os.Getenv("USERPROFILE"), os.Getenv("OneDrive")

	// Known-folder answer first, then the common guesses for when it's unavailable.
	roots := []string{documentsDir()}
	if userProfile != "" {
		roots = append(roots, filepath.Join(userProfile, "Documents"),
			filepath.Join(userProfile, "OneDrive", "Documents"))
	}
	if oneDrive != "" {
		roots = append(roots, filepath.Join(oneDrive, "Documents"))
	}
	if best := pickChatlogs(chatlogsUnder(roots)); best != "" {
		return best
	}

	// Nothing at a known path: sweep the shallow, plausible roots. This is what
	// catches a localized documents folder inside OneDrive that isn't the current
	// redirect target (e.g. an older "Dokument" copy).
	var walkRoots []string
	if userProfile != "" {
		walkRoots = append(walkRoots,
			filepath.Join(userProfile, "Documents"),
			filepath.Join(userProfile, "OneDrive"))
	}
	if oneDrive != "" {
		walkRoots = append(walkRoots, oneDrive)
	}
	return pickChatlogs(walkChatlogs(walkRoots, detectWalkDepth, detectSkip))
}
