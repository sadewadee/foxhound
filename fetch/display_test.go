//go:build playwright

package fetch

import (
	"runtime"
	"testing"
)

func TestNewDisplayManager_NonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("this test verifies non-Linux behavior")
	}

	dm, err := NewDisplayManager()
	if err != nil {
		t.Fatalf("NewDisplayManager on %s should return nil, nil; got err: %v", runtime.GOOS, err)
	}
	if dm != nil {
		t.Fatalf("NewDisplayManager on %s should return nil; got non-nil", runtime.GOOS)
	}
}

func TestNeedsXvfb_NonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("this test verifies non-Linux behavior")
	}

	if needsXvfb() {
		t.Error("needsXvfb() should return false on non-Linux platforms")
	}
}

func TestNeedsXvfb_DisplayAlreadySet(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Xvfb logic only applies on Linux")
	}

	// When DISPLAY is already set, needsXvfb should return false.
	t.Setenv("DISPLAY", ":0")
	if needsXvfb() {
		t.Error("needsXvfb() should return false when DISPLAY is already set")
	}
}

func TestDisplayManager_Close_Nil(t *testing.T) {
	// Close on nil should be safe (no panic).
	var dm *DisplayManager
	if err := dm.Close(); err != nil {
		t.Errorf("Close on nil should return nil; got: %v", err)
	}
}

func TestDisplayManager_Display(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("display string test only meaningful on Linux")
	}

	// We can't start a real Xvfb in CI, but we can test the Display() method
	// on a zero-value DisplayManager.
	dm := &DisplayManager{displayNum: 42}
	if got := dm.Display(); got != ":42" {
		t.Errorf("Display() = %q; want \":42\"", got)
	}
}

func TestCheckShmSize_NonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("this test verifies non-Linux behavior")
	}

	if err := checkShmSize(); err != nil {
		t.Errorf("checkShmSize on %s should return nil; got: %v", runtime.GOOS, err)
	}
}

func TestWithDisplayNumber(t *testing.T) {
	dm := &DisplayManager{displayNum: -1}
	opt := WithDisplayNumber(55)
	opt(dm)
	if dm.displayNum != 55 {
		t.Errorf("WithDisplayNumber(55) set displayNum to %d", dm.displayNum)
	}
}

func TestWithScreenResolution(t *testing.T) {
	dm := &DisplayManager{screenRes: "1920x1080x24"}
	opt := WithScreenResolution("1280x720x16")
	opt(dm)
	if dm.screenRes != "1280x720x16" {
		t.Errorf("WithScreenResolution set screenRes to %q", dm.screenRes)
	}
}
