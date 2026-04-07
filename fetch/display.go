//go:build playwright

// display.go — Xvfb virtual display lifecycle manager.
//
// Manages the Xvfb (X virtual framebuffer) process that Camoufox/Firefox needs
// on Linux servers without a physical display. Handles:
//   - Dynamic display number allocation (avoids collisions)
//   - Process lifecycle (start, health check, restart on crash)
//   - Stale lock file cleanup
//   - Graceful shutdown with signal forwarding
//   - /dev/shm size validation (Firefox needs ≥128 MB)
//
// The display manager is only started when headless mode is "virtual".
// On macOS and Windows, Xvfb is not needed (native headless works).

package fetch

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// DisplayManager manages the lifecycle of an Xvfb virtual display server.
// It is safe for concurrent use.
type DisplayManager struct {
	displayNum int           // X display number (e.g. 99 → ":99")
	cmd        *exec.Cmd     // Xvfb process
	mu         sync.Mutex    // guards cmd and state transitions
	stopped    atomic.Bool   // set to true after Close() is called
	done       chan struct{} // closed when the monitor goroutine exits
	screenRes  string        // screen resolution (e.g. "1920x1080x24")
}

// DisplayOption configures a DisplayManager.
type DisplayOption func(*DisplayManager)

// WithDisplayNumber sets a specific display number instead of auto-allocating.
// Use when you need deterministic display assignment (e.g. testing).
func WithDisplayNumber(n int) DisplayOption {
	return func(d *DisplayManager) {
		d.displayNum = n
	}
}

// WithScreenResolution sets the virtual screen resolution.
// Format: "WIDTHxHEIGHTxDEPTH" (default: "1920x1080x24").
func WithScreenResolution(res string) DisplayOption {
	return func(d *DisplayManager) {
		d.screenRes = res
	}
}

// NewDisplayManager creates and starts an Xvfb virtual display.
//
// On non-Linux platforms, it returns (nil, nil) — Xvfb is not needed because
// macOS has Quartz and Windows has its own display server.
//
// The display number is auto-allocated by scanning /tmp/.X*-lock files to find
// an unused number (starting from 99). If a stale lock file is found (the PID
// in it is not running), the lock file is removed and that display number is
// reused.
func NewDisplayManager(opts ...DisplayOption) (*DisplayManager, error) {
	if runtime.GOOS != "linux" {
		slog.Debug("fetch/display: Xvfb not needed on " + runtime.GOOS)
		return nil, nil
	}

	// Check that Xvfb binary exists before attempting to start.
	xvfbPath, err := exec.LookPath("Xvfb")
	if err != nil {
		return nil, fmt.Errorf("fetch/display: Xvfb not found in PATH — install with: apt-get install xvfb: %w", err)
	}
	_ = xvfbPath

	d := &DisplayManager{
		displayNum: -1, // auto-allocate
		screenRes:  "1920x1080x24",
		done:       make(chan struct{}),
	}
	for _, opt := range opts {
		opt(d)
	}

	// Validate /dev/shm size — Firefox/Camoufox needs at least 128 MB.
	if err := checkShmSize(); err != nil {
		slog.Warn("fetch/display: /dev/shm may be too small for Firefox",
			"err", err,
			"hint", "use --shm-size=256m with docker run")
	}

	// Auto-allocate display number if not specified.
	if d.displayNum < 0 {
		num, allocErr := allocateDisplayNumber()
		if allocErr != nil {
			return nil, fmt.Errorf("fetch/display: allocating display number: %w", allocErr)
		}
		d.displayNum = num
	} else {
		// Clean up stale lock file for the specified display number.
		cleanStaleLock(d.displayNum)
	}

	// Start Xvfb.
	if err := d.start(); err != nil {
		return nil, fmt.Errorf("fetch/display: starting Xvfb on :%d: %w", d.displayNum, err)
	}

	// Set DISPLAY env var so playwright and the browser inherit it.
	displayStr := fmt.Sprintf(":%d", d.displayNum)
	os.Setenv("DISPLAY", displayStr)
	slog.Info("fetch/display: Xvfb started",
		"display", displayStr,
		"screen", d.screenRes,
		"pid", d.cmd.Process.Pid)

	// Start health monitor goroutine.
	go d.monitor()

	return d, nil
}

// Display returns the DISPLAY string (e.g. ":99").
func (d *DisplayManager) Display() string {
	return fmt.Sprintf(":%d", d.displayNum)
}

// start launches the Xvfb process.
func (d *DisplayManager) start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	displayStr := fmt.Sprintf(":%d", d.displayNum)

	// Clean up any stale lock file before starting.
	cleanStaleLock(d.displayNum)

	d.cmd = exec.Command("Xvfb", displayStr,
		"-screen", "0", d.screenRes,
		"-nolisten", "tcp",
		"-ac", // disable access control for simplicity in containers
	)
	// Silence Xvfb output unless debug logging is enabled.
	d.cmd.Stdout = nil
	d.cmd.Stderr = nil

	if err := d.cmd.Start(); err != nil {
		return fmt.Errorf("exec Xvfb: %w", err)
	}

	// Wait briefly to verify Xvfb didn't exit immediately (e.g. display in use).
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- d.cmd.Wait()
	}()

	select {
	case err := <-waitCh:
		// Xvfb exited immediately — display probably in use.
		if err != nil {
			return fmt.Errorf("Xvfb exited immediately: %w", err)
		}
		return errors.New("Xvfb exited immediately with status 0 (unexpected)")
	case <-time.After(500 * time.Millisecond):
		// Xvfb is still running after 500ms — it's healthy.
		// Re-route the wait goroutine: we need to keep collecting the exit status.
		go func() {
			<-waitCh // drain the channel when Xvfb eventually exits
		}()
		return nil
	}
}

// monitor watches the Xvfb process and restarts it if it crashes.
// It runs in a goroutine started by NewDisplayManager and exits when Close() is called.
func (d *DisplayManager) monitor() {
	defer close(d.done)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	consecutiveFailures := 0
	const maxFailures = 5

	for {
		<-ticker.C

		if d.stopped.Load() {
			return
		}

		if !d.isRunning() {
			consecutiveFailures++
			slog.Warn("fetch/display: Xvfb crashed, attempting restart",
				"display", d.Display(),
				"attempt", consecutiveFailures,
				"max_attempts", maxFailures)

			if consecutiveFailures >= maxFailures {
				slog.Error("fetch/display: Xvfb crashed too many times, giving up",
					"display", d.Display(),
					"failures", consecutiveFailures)
				return
			}

			// Brief backoff before restart.
			time.Sleep(time.Duration(consecutiveFailures) * 500 * time.Millisecond)

			if err := d.start(); err != nil {
				slog.Error("fetch/display: failed to restart Xvfb",
					"display", d.Display(),
					"err", err)
				continue
			}

			// Re-set DISPLAY in case something cleared it.
			os.Setenv("DISPLAY", d.Display())
			slog.Info("fetch/display: Xvfb restarted successfully",
				"display", d.Display(),
				"pid", d.cmd.Process.Pid)
			consecutiveFailures = 0
		} else {
			// Reset failure counter on successful health check.
			consecutiveFailures = 0
		}
	}
}

// isRunning checks if the Xvfb process is still alive.
func (d *DisplayManager) isRunning() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cmd == nil || d.cmd.Process == nil {
		return false
	}

	// On Unix, sending signal 0 checks if process exists without affecting it.
	err := d.cmd.Process.Signal(syscall.Signal(0))
	return err == nil
}

// Close stops the Xvfb process and cleans up resources.
// It is safe to call multiple times.
func (d *DisplayManager) Close() error {
	if d == nil {
		return nil
	}
	if d.stopped.Swap(true) {
		return nil // already closed
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	var firstErr error

	if d.cmd != nil && d.cmd.Process != nil {
		// Send SIGTERM first for graceful shutdown.
		if err := d.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			// Process may already be dead — not an error.
			if !errors.Is(err, os.ErrProcessDone) {
				slog.Debug("fetch/display: SIGTERM failed", "err", err)
			}
		} else {
			// Wait up to 3 seconds for graceful exit.
			exitCh := make(chan struct{})
			go func() {
				d.cmd.Process.Wait()
				close(exitCh)
			}()

			select {
			case <-exitCh:
				// Clean exit.
			case <-time.After(3 * time.Second):
				// Force kill.
				if err := d.cmd.Process.Kill(); err != nil {
					if !errors.Is(err, os.ErrProcessDone) {
						firstErr = fmt.Errorf("fetch/display: killing Xvfb: %w", err)
					}
				}
			}
		}
	}

	// Clean up the lock file.
	lockFile := fmt.Sprintf("/tmp/.X%d-lock", d.displayNum)
	if err := os.Remove(lockFile); err != nil && !os.IsNotExist(err) {
		slog.Debug("fetch/display: removing lock file", "file", lockFile, "err", err)
	}

	// Wait for the monitor goroutine to exit.
	select {
	case <-d.done:
	case <-time.After(10 * time.Second):
		slog.Warn("fetch/display: monitor goroutine did not exit within 10s")
	}

	slog.Info("fetch/display: Xvfb stopped", "display", d.Display())
	return firstErr
}

// allocateDisplayNumber finds an unused X display number.
// It starts at 99 and scans upward, checking for existing lock files.
// Stale lock files (where the owning PID is no longer running) are cleaned up
// and their display numbers are reused.
func allocateDisplayNumber() (int, error) {
	for num := 99; num < 200; num++ {
		lockFile := fmt.Sprintf("/tmp/.X%d-lock", num)

		// Check if lock file exists.
		data, err := os.ReadFile(lockFile)
		if err != nil {
			if os.IsNotExist(err) {
				// No lock file — this display number is free.
				return num, nil
			}
			continue // can't read lock file, skip this number
		}

		// Lock file exists — check if the PID is still alive.
		pidStr := strings.TrimSpace(string(data))
		pid, parseErr := strconv.Atoi(pidStr)
		if parseErr != nil {
			// Corrupt lock file — remove and reuse.
			os.Remove(lockFile)
			return num, nil
		}

		// Check if process is still running.
		proc, findErr := os.FindProcess(pid)
		if findErr != nil {
			// Process doesn't exist — stale lock.
			os.Remove(lockFile)
			return num, nil
		}

		// Signal 0 tests if process exists.
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			// Process is dead — stale lock.
			os.Remove(lockFile)
			slog.Debug("fetch/display: cleaned stale lock file",
				"file", lockFile, "dead_pid", pid)
			return num, nil
		}

		// Process is alive — this display is in use, try next.
	}

	return 0, errors.New("no available display numbers in range :99-:199")
}

// cleanStaleLock removes a lock file if the owning PID is no longer running.
func cleanStaleLock(displayNum int) {
	lockFile := fmt.Sprintf("/tmp/.X%d-lock", displayNum)

	data, err := os.ReadFile(lockFile)
	if err != nil {
		return // no lock file or can't read it
	}

	pidStr := strings.TrimSpace(string(data))
	pid, parseErr := strconv.Atoi(pidStr)
	if parseErr != nil {
		// Corrupt lock file — remove it.
		os.Remove(lockFile)
		return
	}

	proc, findErr := os.FindProcess(pid)
	if findErr != nil {
		os.Remove(lockFile)
		return
	}

	if err := proc.Signal(syscall.Signal(0)); err != nil {
		os.Remove(lockFile)
		slog.Debug("fetch/display: cleaned stale lock file",
			"file", lockFile, "dead_pid", pid)
	}
}

// checkShmSize validates that /dev/shm has enough space for Firefox.
// Firefox/Camoufox uses shared memory extensively for IPC between the main
// process and content processes. With the Docker default of 64 MB, Firefox
// can crash with "out of memory" errors.
func checkShmSize() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	// Read /dev/shm size from /proc/mounts or df.
	// Simplest approach: check if /dev/shm exists and has reasonable free space.
	info, err := os.Stat("/dev/shm")
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("/dev/shm does not exist — Firefox may crash")
		}
		return nil // can't check, don't warn
	}

	if !info.IsDir() {
		return errors.New("/dev/shm is not a directory")
	}

	// Try to determine available space using syscall.Statfs.
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/dev/shm", &stat); err != nil {
		return nil // can't check, don't warn
	}

	availableBytes := stat.Bavail * uint64(stat.Bsize)
	availableMB := availableBytes / (1024 * 1024)

	if availableMB < 128 {
		return fmt.Errorf("/dev/shm has only %d MB available (need ≥128 MB for Firefox — use --shm-size=256m)", availableMB)
	}

	return nil
}

// needsXvfb returns true if the current platform requires Xvfb for headless
// browser operation in "virtual" mode.
func needsXvfb() bool {
	if runtime.GOOS != "linux" {
		return false
	}

	// Check if a DISPLAY is already set (e.g. user has a desktop session
	// or an external Xvfb is running).
	if display := os.Getenv("DISPLAY"); display != "" {
		slog.Debug("fetch/display: DISPLAY already set, skipping Xvfb",
			"display", display)
		return false
	}

	return true
}

// xvfbDisplayPath returns the Unix socket path for the given display number.
// Used for cleanup verification.
func xvfbDisplayPath(displayNum int) string {
	return filepath.Join("/tmp", fmt.Sprintf(".X11-unix/X%d", displayNum))
}
