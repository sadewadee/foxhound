package fetch

import (
	"errors"
	"net"
	"testing"
)

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"reset", errors.New("connection reset by peer"), true},
		{"timeout", errors.New("i/o timeout"), true},
		{"EOF", errors.New("unexpected EOF"), true},
		{"refused", errors.New("connection refused"), true},
		{"broken pipe", errors.New("write: broken pipe"), true},
		{"dns not found", errors.New("no such host"), true},
		{"permanent", errors.New("tls: certificate invalid"), false},
		{"http error", errors.New("status 404"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTransientError(tt.err); got != tt.want {
				t.Errorf("isTransientError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestIsTransientError_NetError verifies that a net.Error with Timeout() == true
// is classified as transient even when the error message doesn't match any pattern.
func TestIsTransientError_NetError(t *testing.T) {
	err := &net.DNSError{IsTimeout: true, Name: "example.com"}
	if !isTransientError(err) {
		t.Errorf("isTransientError(net.DNSError{IsTimeout:true}) = false, want true")
	}
}
