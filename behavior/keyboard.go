package behavior

import (
	"math/rand/v2"
	"time"
)

// KeyboardConfig configures keyboard typing simulation.
type KeyboardConfig struct {
	// MinDelay is the minimum inter-key delay (default 50 ms).
	MinDelay time.Duration
	// MaxDelay is the maximum inter-key delay (default 200 ms).
	MaxDelay time.Duration
	// TypoProb is the per-character probability of introducing a typo followed
	// by a backspace correction (default 0.02).
	TypoProb float64
}

// DefaultKeyboardConfig returns architecture-recommended defaults.
func DefaultKeyboardConfig() KeyboardConfig {
	return KeyboardConfig{
		MinDelay: 50 * time.Millisecond,
		MaxDelay: 200 * time.Millisecond,
		TypoProb: 0.02,
	}
}

// KeyAction represents a single keystroke event in a typing sequence.
type KeyAction struct {
	// Char is the character to type.  For backspace actions Char is 0.
	Char rune
	// Delay is the pause before this keystroke.
	Delay time.Duration
	// IsBackspace is true when this action represents a correction keystroke.
	IsBackspace bool
}

// Keyboard generates human-like typing sequences.
type Keyboard struct {
	config KeyboardConfig
}

// NewKeyboard creates a Keyboard with the supplied configuration.
func NewKeyboard(cfg KeyboardConfig) *Keyboard {
	return &Keyboard{config: cfg}
}

// TypeString converts text into a sequence of KeyActions that, when played
// back in order, reproduce the intended string.
//
// For each rune in text:
//  1. With probability TypoProb a random adjacent-key typo is inserted,
//     followed immediately by a backspace and the correct character.
//  2. Every keystroke (including backspace) receives a realistic delay
//     sampled uniformly from [MinDelay, MaxDelay].
func (k *Keyboard) TypeString(text string) []KeyAction {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}

	actions := make([]KeyAction, 0, len(runes))

	for _, ch := range runes {
		// Possibly insert a typo before the correct character.
		if k.config.TypoProb > 0 && rand.Float64() < k.config.TypoProb {
			typoChar := adjacentTypo(ch)
			actions = append(actions, KeyAction{
				Char:        typoChar,
				Delay:       k.keyDelay(),
				IsBackspace: false,
			})
			// Backspace to erase the typo.
			actions = append(actions, KeyAction{
				Char:        0,
				Delay:       k.keyDelay(),
				IsBackspace: true,
			})
		}

		actions = append(actions, KeyAction{
			Char:        ch,
			Delay:       k.keyDelay(),
			IsBackspace: false,
		})
	}

	return actions
}

// keyDelay returns a uniformly-sampled inter-key delay in [MinDelay, MaxDelay].
func (k *Keyboard) keyDelay() time.Duration {
	minMs := float64(k.config.MinDelay.Milliseconds())
	maxMs := float64(k.config.MaxDelay.Milliseconds())
	ms := minMs + rand.Float64()*(maxMs-minMs)
	return time.Duration(ms) * time.Millisecond
}

// adjacentTypo returns a plausible mis-typed character near ch on a QWERTY
// layout.  Falls back to the original character if no neighbour is known so
// that the backspace sequence still makes sense structurally.
func adjacentTypo(ch rune) rune {
	neighbours := map[rune]string{
		'a': "sqwz", 'b': "vghn", 'c': "xdfv", 'd': "serfcx",
		'e': "wrsdf", 'f': "drtgvc", 'g': "ftyhbv", 'h': "gyujbn",
		'i': "uojkl", 'j': "huikmn", 'k': "jiolm", 'l': "kop",
		'm': "njk", 'n': "bhjm", 'o': "iklp", 'p': "ol",
		'q': "wa", 'r': "etdf", 's': "qaewdz", 't': "ryfg",
		'u': "yhij", 'v': "cfgb", 'w': "qase", 'x': "zsdc",
		'y': "tugh", 'z': "asx",
	}
	lower := ch
	if ch >= 'A' && ch <= 'Z' {
		lower = ch - 'A' + 'a'
	}
	ns, ok := neighbours[lower]
	if !ok || len(ns) == 0 {
		return ch
	}
	picked := rune(ns[rand.IntN(len(ns))])
	// Preserve original case.
	if ch >= 'A' && ch <= 'Z' {
		picked = picked - 'a' + 'A'
	}
	return picked
}
