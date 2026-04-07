//go:build !playwright

// display_stub.go — stub display manager for non-playwright builds.
// Provides the same public types and constructors as display.go but
// all operations are no-ops.

package fetch

// DisplayManager is a no-op stub when playwright is not enabled.
type DisplayManager struct{}

// DisplayOption is a functional option (no-op in stub build).
type DisplayOption func(*DisplayManager)

// WithDisplayNumber is a no-op in the stub build.
func WithDisplayNumber(n int) DisplayOption {
	return func(d *DisplayManager) {}
}

// WithScreenResolution is a no-op in the stub build.
func WithScreenResolution(res string) DisplayOption {
	return func(d *DisplayManager) {}
}

// NewDisplayManager returns nil in the stub build — Xvfb is never needed
// without playwright.
func NewDisplayManager(opts ...DisplayOption) (*DisplayManager, error) {
	return nil, nil
}

// Display returns an empty string in the stub build.
func (d *DisplayManager) Display() string { return "" }

// Close is a no-op in the stub build.
func (d *DisplayManager) Close() error { return nil }
