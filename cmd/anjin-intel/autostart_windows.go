//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/polarn/anjin-intel/internal/config"
)

type windowsAutostart struct{}

func newAutostarter() autostarter { return windowsAutostart{} }

func currentUser() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.Username, nil // DOMAIN\user on Windows
}

// Register writes the task XML and registers it through one elevated schtasks call.
//
// Elevation is unavoidable: on an Administrator account a normal terminal holds the
// UAC-FILTERED token, where BUILTIN\Administrators is "deny only", and the Task
// Scheduler library grants create rights to Administrators rather than Users — so
// `schtasks /create` returns "Access is denied" for the very user who owns the machine.
// Only this step is elevated; the config and binary were already written unelevated,
// so the enrollment token never crosses the boundary onto an elevated command line.
func (windowsAutostart) Register(bin string) error {
	who, err := currentUser()
	if err != nil {
		return fmt.Errorf("resolve current user: %w", err)
	}
	doc, err := renderTaskXML(bin, who)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "anjin-intel-task")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	xmlPath := filepath.Join(dir, "task.xml")
	logPath := filepath.Join(dir, "schtasks.log")
	if err := os.WriteFile(xmlPath, utf16LEWithBOM(doc), 0o600); err != nil {
		return err
	}

	// Create and start in a single elevated shell so the user sees ONE UAC prompt.
	// Output is redirected to a file because the elevated window is hidden — without
	// it a schtasks failure would be invisible.
	args := fmt.Sprintf(`/c schtasks /create /tn "%s" /xml "%s" /f > "%s" 2>&1 && schtasks /run /tn "%s" >> "%s" 2>&1`,
		taskName, xmlPath, logPath, taskName, logPath)
	code, err := runElevated("cmd.exe", args)
	if err != nil {
		return err
	}
	if code != 0 {
		out, _ := os.ReadFile(logPath)
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("schtasks failed (exit %d): %s", code, msg)
		}
		return fmt.Errorf("schtasks failed (exit %d)", code)
	}
	return nil
}

// Stop halts a running shipper so its .exe can be replaced — Windows locks a running
// image, so this is required before install overwrites it. Best-effort throughout:
// `schtasks /end` may itself need elevation, and taskkill is the fallback that works
// unelevated against our own process.
func (windowsAutostart) Stop() error {
	_ = exec.Command("schtasks", "/end", "/tn", taskName).Run()
	// Exclude our own PID or this kills the installer: `install` runs from a binary
	// called anjin-intel.exe, so an unfiltered /IM match names the very process asking
	// for the kill. It dies silently mid-install, before writing any config — no panic,
	// no message, just a bare non-zero exit.
	_ = exec.Command("taskkill", "/IM", config.BinName(), "/F", "/FI", notSelfFilter()).Run()
	// Termination is asynchronous; wait briefly for the image lock to drop.
	for i := 0; i < 20; i++ {
		if !processRunning() {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return nil
}

func (windowsAutostart) Remove() error {
	if !taskRegistered() {
		return nil
	}
	_ = windowsAutostart{}.Stop()
	dir, err := os.MkdirTemp("", "anjin-intel-task")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	logPath := filepath.Join(dir, "schtasks.log")
	args := fmt.Sprintf(`/c schtasks /delete /tn "%s" /f > "%s" 2>&1`, taskName, logPath)
	code, err := runElevated("cmd.exe", args)
	if err != nil {
		return err
	}
	if code != 0 {
		out, _ := os.ReadFile(logPath)
		return fmt.Errorf("schtasks /delete failed (exit %d): %s", code, strings.TrimSpace(string(out)))
	}
	return nil
}

// State reports registration and liveness WITHOUT parsing schtasks' human-readable
// output: the Status field is localized, and this machine is the exact trap — an
// English UI with a Swedish user locale (the same split that makes Documents
// "Dokument"). An exit code and an image-name match are language-independent.
func (windowsAutostart) State() string {
	reg, run := taskRegistered(), processRunning()
	switch {
	case reg && run:
		return "registered (Task Scheduler), running"
	case reg:
		return "registered (Task Scheduler), not running"
	case run:
		return "not registered, but a shipper is running"
	default:
		return "not registered — run `anjin-intel install`"
	}
}

func taskRegistered() bool {
	return exec.Command("schtasks", "/query", "/tn", taskName).Run() == nil
}

// notSelfFilter is a taskkill/tasklist filter excluding this process. Both commands
// match on image name, and every subcommand here runs from that same image.
func notSelfFilter() string { return "PID ne " + strconv.Itoa(os.Getpid()) }

// processRunning reports whether a shipper OTHER than this process is live. Without the
// PID exclusion `status` would always claim the shipper is running — it would be seeing
// itself.
func processRunning() bool {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+config.BinName(), "/FI", notSelfFilter(), "/NH").Output()
	if err != nil {
		return false
	}
	// No match prints an INFO line rather than failing, so look for the name itself.
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(config.BinName()))
}

// --- elevation ---

var (
	modshell32b = syscall.NewLazyDLL("shell32.dll")
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")

	procShellExecuteExW    = modshell32b.NewProc("ShellExecuteExW")
	procWaitForSingleObj   = modkernel32.NewProc("WaitForSingleObject")
	procGetExitCodeProcess = modkernel32.NewProc("GetExitCodeProcess")
	procCloseHandle        = modkernel32.NewProc("CloseHandle")
)

const (
	seeMaskNoCloseProcess = 0x00000040
	swHide                = 0
	infinite              = 0xFFFFFFFF
	errorCancelled        = 1223 // ERROR_CANCELLED — the user dismissed the UAC prompt
)

type shellExecuteInfoW struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     uintptr
}

// runElevated runs exe with the "runas" verb (a UAC prompt) and waits for it, returning
// its exit code. ShellExecuteExW rather than ShellExecuteW because only the Ex form
// hands back a process handle — without it we could not tell whether schtasks actually
// succeeded, only that something was launched.
func runElevated(exe, params string) (int, error) {
	verbp, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return 0, err
	}
	exep, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		return 0, err
	}
	parp, err := syscall.UTF16PtrFromString(params)
	if err != nil {
		return 0, err
	}
	sei := shellExecuteInfoW{
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verbp,
		lpFile:       exep,
		lpParameters: parp,
		nShow:        swHide,
	}
	sei.cbSize = uint32(unsafe.Sizeof(sei))

	r1, _, e := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	if r1 == 0 {
		if errno, ok := e.(syscall.Errno); ok && errno == errorCancelled {
			return 0, errElevationDeclined
		}
		return 0, fmt.Errorf("elevation failed: %v", e)
	}
	if sei.hProcess == 0 {
		return 0, fmt.Errorf("elevation returned no process handle")
	}
	defer procCloseHandle.Call(sei.hProcess)

	procWaitForSingleObj.Call(sei.hProcess, uintptr(uint32(infinite)))
	var code uint32
	if r, _, e := procGetExitCodeProcess.Call(sei.hProcess, uintptr(unsafe.Pointer(&code))); r == 0 {
		return 0, fmt.Errorf("could not read exit code: %v", e)
	}
	return int(code), nil
}
