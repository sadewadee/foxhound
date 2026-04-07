//go:build !playwright

package fetch

import (
	"testing"
)

func TestDisplayManager_Stub_NewReturnsNil(t *testing.T) {
	dm, err := NewDisplayManager()
	if err != nil {
		t.Fatalf("stub NewDisplayManager should return nil, nil; got err: %v", err)
	}
	if dm != nil {
		t.Fatal("stub NewDisplayManager should return nil")
	}
}

func TestDisplayManager_Stub_CloseNil(t *testing.T) {
	var dm *DisplayManager
	if err := dm.Close(); err != nil {
		t.Errorf("stub Close on nil should return nil; got: %v", err)
	}
}

func TestDisplayManager_Stub_Display(t *testing.T) {
	dm := &DisplayManager{}
	if got := dm.Display(); got != "" {
		t.Errorf("stub Display() = %q; want empty string", got)
	}
}

func TestDisplayManager_Stub_WithOptions(t *testing.T) {
	// Options should be accepted without panic.
	dm := &DisplayManager{}
	WithDisplayNumber(42)(dm)
	WithScreenResolution("1280x720x16")(dm)
	// No-op, just ensure no panic.
}
