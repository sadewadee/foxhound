// sleep_ctx.go — context-aware sleep helper used on the captcha / Cloudflare
// solve path. Callers use this instead of time.Sleep so that hunt cancellation
// does not have to wait out the full solve budget (up to 90s for reCAPTCHA).

package fetch

import (
	"context"
	"time"
)

// sleepCtx blocks for d or until ctx is cancelled, whichever comes first.
// Returns true if the full duration elapsed, false if ctx was cancelled
// (either before or during the sleep).
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
