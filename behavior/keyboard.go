package behavior

import (
	"math"
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
	// BigramModel enables the bigram-aware typing model that adjusts inter-key
	// delay based on character frequency, hand/finger transitions, and position
	// fatigue. When false, the original uniform delay is used.
	BigramModel bool
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
//  2. When BigramModel is enabled, inter-key delay is computed from character
//     frequency, hand/finger transitions, and position fatigue. Otherwise a
//     uniform delay in [MinDelay, MaxDelay] is used.
func (k *Keyboard) TypeString(text string) []KeyAction {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}

	actions := make([]KeyAction, 0, len(runes))
	var prev rune

	for i, ch := range runes {
		if k.config.TypoProb > 0 && rand.Float64() < k.config.TypoProb {
			typoChar := adjacentTypo(ch)
			actions = append(actions, KeyAction{
				Char:        typoChar,
				Delay:       k.charDelay(prev, typoChar, i),
				IsBackspace: false,
			})
			actions = append(actions, KeyAction{
				Char:        0,
				Delay:       k.charDelay(prev, ch, i),
				IsBackspace: true,
			})
		}

		actions = append(actions, KeyAction{
			Char:        ch,
			Delay:       k.charDelay(prev, ch, i),
			IsBackspace: false,
		})
		prev = ch
	}

	return actions
}

// charDelay returns the delay for a keystroke, using bigram model if enabled.
func (k *Keyboard) charDelay(prev, ch rune, position int) time.Duration {
	if k.config.BigramModel && prev != 0 {
		return k.bigramDelay(prev, ch, position)
	}
	return k.keyDelay()
}

// keyDelay returns a uniformly-sampled inter-key delay in [MinDelay, MaxDelay].
func (k *Keyboard) keyDelay() time.Duration {
	minMs := float64(k.config.MinDelay.Milliseconds())
	maxMs := float64(k.config.MaxDelay.Milliseconds())
	ms := minMs + rand.Float64()*(maxMs-minMs)
	return time.Duration(ms) * time.Millisecond
}

// fingerMap maps each lowercase letter to [hand, finger] for QWERTY layout.
// hand: 0=left, 1=right. finger: 0=pinky, 1=ring, 2=middle, 3=index.
var fingerMap = map[rune][2]int{
	'a': {0, 0}, 's': {0, 1}, 'd': {0, 2}, 'f': {0, 3},
	'g': {0, 3}, 'h': {1, 3}, 'j': {1, 3}, 'k': {1, 2},
	'l': {1, 1}, 'q': {0, 0}, 'w': {0, 1}, 'e': {0, 2},
	'r': {0, 3}, 't': {0, 3}, 'y': {1, 3}, 'u': {1, 3},
	'i': {1, 2}, 'o': {1, 1}, 'p': {1, 0}, 'z': {0, 0},
	'x': {0, 1}, 'c': {0, 2}, 'v': {0, 3}, 'b': {0, 3},
	'n': {1, 3}, 'm': {1, 3},
}

// charFreqRank maps letters to frequency rank (0=most common).
var charFreqRank = map[rune]int{
	'e': 0, 't': 1, 'a': 2, 'o': 3, 'i': 4, 'n': 5,
	's': 6, 'h': 7, 'r': 8, 'd': 9, 'l': 10, 'c': 11,
	'u': 12, 'm': 13, 'w': 14, 'f': 15, 'g': 16, 'y': 17,
	'p': 18, 'b': 19, 'v': 20, 'k': 21, 'j': 22, 'x': 23,
	'q': 24, 'z': 25,
}

// bigramDelay computes a human-like inter-key delay based on character frequency,
// hand/finger transitions, and typing position fatigue.
//
// Formula: delay(P, C) = base * freqFactor(C) * bigramFactor(P, C) * fatigueFactor(pos)
// Result is sampled from LogNormal(ln(computed), 0.15) for per-keystroke variance.
func (k *Keyboard) bigramDelay(prev, ch rune, position int) time.Duration {
	base := float64(k.config.MinDelay+k.config.MaxDelay) / 2.0 // midpoint as base

	// Frequency factor: common letters are faster (0.8), rare letters slower (1.2)
	ff := 1.0
	lower := ch
	if ch >= 'A' && ch <= 'Z' {
		lower = ch - 'A' + 'a'
	}
	if rank, ok := charFreqRank[lower]; ok {
		ff = 0.8 + 0.4*(float64(rank)/26.0)
	}

	// Bigram transition factor based on hand/finger assignment
	bf := 1.0
	prevLower := prev
	if prev >= 'A' && prev <= 'Z' {
		prevLower = prev - 'A' + 'a'
	}
	pf, pOk := fingerMap[prevLower]
	cf, cOk := fingerMap[lower]
	if pOk && cOk {
		if pf[0] != cf[0] {
			// Different hand: fastest (parallel motion)
			bf = 0.85
		} else if pf[1] == cf[1] {
			// Same finger: slowest (finger must travel)
			bf = 2.0
		} else {
			diff := pf[1] - cf[1]
			if diff < 0 {
				diff = -diff
			}
			if diff == 1 {
				// Adjacent finger
				bf = 1.0
			} else {
				// Distant finger (slight reach)
				bf = 0.95
			}
		}
	}

	// Fatigue factor: gradual slowdown over long strings
	fatigue := 1.0 + 0.001*float64(position)

	computed := base * ff * bf * fatigue

	// Sample from LogNormal for per-keystroke variance
	sampled := LogNormalSample(math.Log(computed), 0.35)

	// Clamp to reasonable bounds
	minNs := float64(k.config.MinDelay) * 0.5
	maxNs := float64(k.config.MaxDelay) * 2.0
	if sampled < minNs {
		sampled = minNs
	}
	if sampled > maxNs {
		sampled = maxNs
	}

	return time.Duration(sampled)
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
