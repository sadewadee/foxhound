package fetch

import (
	"errors"
	"net"
	"strings"
	"syscall"
)

// isTransientError returns true for network errors that are likely temporary
// and worth retrying: connection reset, timeout, refused, EOF, broken pipe.
func isTransientError(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()
	transientPatterns := []string{
		"reset",
		"timeout",
		"refused",
		"EOF",
		"broken pipe",
		"connection closed",
		"no such host", // DNS can be transient
		"network is unreachable",
		"i/o timeout",
	}
	for _, p := range transientPatterns {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(p)) {
			return true
		}
	}

	// Check for specific syscall errors
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}

	return false
}
