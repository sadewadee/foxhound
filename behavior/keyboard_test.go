package behavior

import (
	"testing"
	"time"
	"unicode/utf8"
)

func TestNewKeyboardReturnsNonNil(t *testing.T) {
	k := NewKeyboard(DefaultKeyboardConfig())
	if k == nil {
		t.Fatal("NewKeyboard returned nil")
	}
}

func TestTypeStringProducesCorrectCharacters(t *testing.T) {
	k := NewKeyboard(DefaultKeyboardConfig())
	text := "hello"
	actions := k.TypeString(text)

	// Collect non-backspace characters in order.
	var result []rune
	for _, a := range actions {
		if a.IsBackspace {
			if len(result) > 0 {
				result = result[:len(result)-1]
			}
		} else {
			result = append(result, a.Char)
		}
	}

	got := string(result)
	if got != text {
		t.Errorf("TypeString(%q) final text = %q, want %q", text, got, text)
	}
}

func TestTypeStringDelaysBetweenKeys(t *testing.T) {
	cfg := DefaultKeyboardConfig()
	k := NewKeyboard(cfg)
	actions := k.TypeString("abcde")

	for i, a := range actions {
		if !a.IsBackspace && a.Delay < cfg.MinDelay {
			t.Errorf("action[%d] delay %v below MinDelay %v", i, a.Delay, cfg.MinDelay)
		}
		if !a.IsBackspace && a.Delay > cfg.MaxDelay {
			t.Errorf("action[%d] delay %v above MaxDelay %v", i, a.Delay, cfg.MaxDelay)
		}
	}
}

func TestTypeStringHandlesEmptyString(t *testing.T) {
	k := NewKeyboard(DefaultKeyboardConfig())
	actions := k.TypeString("")
	if len(actions) != 0 {
		t.Errorf("TypeString(\"\") returned %d actions, want 0", len(actions))
	}
}

func TestTypeStringBackspaceIsFollowedByCorrection(t *testing.T) {
	// High typo probability to force at least one correction in a large input.
	cfg := KeyboardConfig{
		MinDelay: 50 * time.Millisecond,
		MaxDelay: 200 * time.Millisecond,
		TypoProb: 1.0, // every character gets a typo
	}
	k := NewKeyboard(cfg)
	longText := "abcdefghijklmnopqrstuvwxyz"
	actions := k.TypeString(longText)

	hasBackspace := false
	for _, a := range actions {
		if a.IsBackspace {
			hasBackspace = true
			break
		}
	}
	if !hasBackspace {
		t.Error("TypoProb=1.0 produced no backspace actions")
	}
}

func TestTypeStringUnicodeCharacters(t *testing.T) {
	k := NewKeyboard(DefaultKeyboardConfig())
	text := "café"
	actions := k.TypeString(text)

	var result []rune
	for _, a := range actions {
		if a.IsBackspace {
			if len(result) > 0 {
				result = result[:len(result)-1]
			}
		} else {
			result = append(result, a.Char)
		}
	}

	got := string(result)
	if got != text {
		t.Errorf("TypeString(%q) = %q, want %q", text, got, text)
	}
}

func TestTypeStringActionCountIsReasonable(t *testing.T) {
	// With TypoProb=0, actions count == len(runes).
	cfg := KeyboardConfig{
		MinDelay: 50 * time.Millisecond,
		MaxDelay: 200 * time.Millisecond,
		TypoProb: 0,
	}
	k := NewKeyboard(cfg)
	text := "foxhound"
	actions := k.TypeString(text)

	runeCount := utf8.RuneCountInString(text)
	if len(actions) != runeCount {
		t.Errorf("TypoProb=0: got %d actions for %d rune text, want exact match", len(actions), runeCount)
	}
}

func TestTypeStringBackspaceDelayIsPositive(t *testing.T) {
	cfg := KeyboardConfig{
		MinDelay: 50 * time.Millisecond,
		MaxDelay: 200 * time.Millisecond,
		TypoProb: 1.0,
	}
	k := NewKeyboard(cfg)
	actions := k.TypeString("hello")
	for i, a := range actions {
		if a.IsBackspace && a.Delay <= 0 {
			t.Errorf("action[%d] backspace has non-positive delay %v", i, a.Delay)
		}
	}
}
