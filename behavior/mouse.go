package behavior

import (
	"math"
	"math/rand/v2"
	"time"
)

// Point represents a 2-D screen coordinate.
type Point struct {
	X, Y float64
}

// MouseConfig configures mouse movement generation.
type MouseConfig struct {
	// Jitter is the maximum per-point micro-jitter in pixels (default 2.0).
	Jitter float64
	// OvershootProb is the probability that the cursor overshoots the target
	// before correcting back (default 0.2).
	OvershootProb float64
	// OvershootPx is the maximum overshoot distance in pixels (default 3.0).
	OvershootPx float64
}

// DefaultMouseConfig returns the architecture-recommended defaults.
func DefaultMouseConfig() MouseConfig {
	return MouseConfig{
		Jitter:        2.0,
		OvershootProb: 0.2,
		OvershootPx:   3.0,
	}
}

// Mouse generates human-like mouse-movement trajectories.
type Mouse struct {
	config MouseConfig
}

// NewMouse creates a Mouse with the supplied configuration.
func NewMouse(cfg MouseConfig) *Mouse {
	return &Mouse{config: cfg}
}

// MoveTo generates a bezier-curve path from start to end.
//
// Implementation details:
//  1. 3-5 random control points are placed between start and end, offset
//     perpendicular to the straight line to create natural curvature.
//  2. The bezier curve is sampled at 20-50 evenly-spaced t values.
//  3. Each sampled point receives independent micro-jitter (±Jitter px).
//  4. Speed profile: slow at endpoints, fast in middle (ease-in-out via
//     smoothstep remapping of t).
//  5. With probability OvershootProb the final point overshoots by up to
//     OvershootPx px, then a correction segment brings it back to end.
func (m *Mouse) MoveTo(start, end Point) []Point {
	// Handle zero-length movement: return a single jittered point.
	dx := end.X - start.X
	dy := end.Y - start.Y
	if dx == 0 && dy == 0 {
		return []Point{m.jitter(start)}
	}

	// Number of control points: 3-5 (inclusive), sampled uniformly.
	nCtrl := 3 + rand.IntN(3) // 3,4,5

	// Build control-point sequence: start, intermediate pts, end.
	ctrl := make([]Point, nCtrl+2)
	ctrl[0] = start
	ctrl[len(ctrl)-1] = end

	// Perpendicular unit vector for offset.
	length := math.Sqrt(dx*dx + dy*dy)
	perpX := -dy / length
	perpY := dx / length

	for i := 1; i <= nCtrl; i++ {
		t := float64(i) / float64(nCtrl+1)
		// Base point along the straight line.
		base := Point{
			X: start.X + t*dx,
			Y: start.Y + t*dy,
		}
		// Perpendicular offset: ±30% of total length at most.
		maxOffset := length * 0.30
		offset := (rand.Float64()*2 - 1) * maxOffset
		ctrl[i] = Point{
			X: base.X + perpX*offset,
			Y: base.Y + perpY*offset,
		}
	}

	// Number of samples: 20-50.
	nSamples := 20 + rand.IntN(31)

	path := make([]Point, 0, nSamples)
	for i := 0; i < nSamples; i++ {
		t := float64(i) / float64(nSamples-1)
		// Ease-in-out remapping: smooth acceleration then deceleration.
		tEased := smoothstep(t)
		pt := evalBezier(ctrl, tEased)
		path = append(path, m.jitter(pt))
	}

	// Overshoot: extend past end then snap back.
	if rand.Float64() < m.config.OvershootProb {
		over := m.config.OvershootPx * rand.Float64()
		norm := math.Sqrt(dx*dx + dy*dy)
		overshootPt := Point{
			X: end.X + (dx/norm)*over,
			Y: end.Y + (dy/norm)*over,
		}
		path = append(path, m.jitter(overshootPt))
		// Correction back to end.
		path = append(path, m.jitter(end))
	}

	return path
}

// ClickOffset returns a small random offset from an element's centre.
// Range: 0-5 px in each axis, matching architecture spec.
func (m *Mouse) ClickOffset() Point {
	return Point{
		X: (rand.Float64()*2 - 1) * 5,
		Y: (rand.Float64()*2 - 1) * 5,
	}
}

// ClickDuration returns the duration between mouse-down and mouse-up.
// Range: [50 ms, 150 ms].
func (m *Mouse) ClickDuration() time.Duration {
	ms := 50 + rand.Float64()*(150-50)
	return time.Duration(ms * float64(time.Millisecond))
}

// IdleDrift returns a small random drift suitable for idle mouse simulation.
// Range: ≤ 2 px in each axis.
func (m *Mouse) IdleDrift() Point {
	return Point{
		X: (rand.Float64()*2 - 1) * 2,
		Y: (rand.Float64()*2 - 1) * 2,
	}
}

// jitter adds uniformly-distributed noise in [-Jitter, +Jitter] to each axis.
func (m *Mouse) jitter(p Point) Point {
	if m.config.Jitter == 0 {
		return p
	}
	j := m.config.Jitter
	return Point{
		X: p.X + (rand.Float64()*2-1)*j,
		Y: p.Y + (rand.Float64()*2-1)*j,
	}
}

// smoothstep maps t ∈ [0,1] to the ease-in-out cubic curve 3t²-2t³.
// This produces slow movement near t=0 and t=1, fast in the middle.
func smoothstep(t float64) float64 {
	return t * t * (3 - 2*t)
}

// evalBezier evaluates the De Casteljau algorithm for an arbitrary-degree
// bezier curve defined by pts at parameter t ∈ [0,1].
func evalBezier(pts []Point, t float64) Point {
	n := len(pts)
	tmp := make([]Point, n)
	copy(tmp, pts)
	for step := 1; step < n; step++ {
		for i := 0; i < n-step; i++ {
			tmp[i] = Point{
				X: (1-t)*tmp[i].X + t*tmp[i+1].X,
				Y: (1-t)*tmp[i].Y + t*tmp[i+1].Y,
			}
		}
	}
	return tmp[0]
}
