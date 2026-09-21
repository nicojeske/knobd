package audio

import (
	"math"
	"testing"
)

func TestCurveAdjustLinearDefault(t *testing.T) {
	// Exponent 1 (the default, per the M03 spec) must reduce to plain
	// addition in the pavucontrol/KDE percent scale — that's the whole
	// point of choosing linear over the originally-guessed cubic.
	cases := []struct {
		name    string
		current float64
		step    float64
		want    float64
	}{
		{"typical step up", 50, 2, 52},
		{"typical step down", 50, -2, 48},
		{"negative step clamps at zero, not below", 1, -5, 0},
		{"already at zero, negative step stays zero", 0, -5, 0},
		{"step past the default ceiling clamps at 150", 149, 10, DefaultMaxPercent},
		{"exactly at the ceiling", 150, 1, DefaultMaxPercent},
		{"zero step is a no-op", 73, 0, 73},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Curve{}
			got := c.Adjust(tc.current, tc.step)
			if got != tc.want {
				t.Errorf("Adjust(%v, %v) = %v, want %v", tc.current, tc.step, got, tc.want)
			}
		})
	}
}

func TestCurveAdjustExponentSanitization(t *testing.T) {
	// A hand-edited curveExponent of 0 means "use the engine default"
	// per VolumeAdjustAction's doc comment, and a negative one means
	// nothing sensible — both must behave exactly like Exponent: 1.
	for _, gamma := range []float64{0, -1, -100} {
		c := Curve{Exponent: gamma}
		got := c.Adjust(50, 2)
		if got != 52 {
			t.Errorf("Curve{Exponent: %v}.Adjust(50, 2) = %v, want 52 (sanitized to linear)", gamma, got)
		}
	}
}

func TestCurveAdjustNonLinearPinnedValues(t *testing.T) {
	// Pinned against the same position-space formula computed
	// independently in Python, to catch a regression in the Go
	// implementation rather than merely restate it.
	cases := []struct {
		name    string
		gamma   float64
		current float64
		step    float64
		want    float64
	}{
		{"gamma 3, fine at the low end", 3, 10, 2, 11.3491598800},
		{"gamma 3, coarse at the high end", 3, 90, 2, 95.7096772369},
		{"gamma 0.5, midrange", 0.5, 50, 5, 54.7722557505},
	}
	const epsilon = 1e-6
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Curve{Exponent: tc.gamma}
			got := c.Adjust(tc.current, tc.step)
			if math.Abs(got-tc.want) > epsilon {
				t.Errorf("Adjust(%v, %v) with gamma=%v = %v, want %v", tc.current, tc.step, tc.gamma, got, tc.want)
			}
		})
	}
}

func TestCurveAdjustNeverProducesNaNOrNegative(t *testing.T) {
	// math.Pow(negative, 1/gamma) is NaN; a negative or NaN result must
	// never reach the caller (it would otherwise round-trip through
	// NormVolume into an enormous or undefined volume).
	inputs := []float64{-50, -1, math.NaN(), math.Inf(-1)}
	for _, gamma := range []float64{1, 2, 3, 0.5} {
		for _, current := range inputs {
			c := Curve{Exponent: gamma}
			got := c.Adjust(current, -10)
			if math.IsNaN(got) || got < 0 {
				t.Errorf("Curve{Exponent:%v}.Adjust(%v, -10) = %v, want a finite value >= 0", gamma, current, got)
			}
		}
	}
}

func TestCurveAdjustCustomMaxPercent(t *testing.T) {
	c := Curve{MaxPercent: 120}
	if got := c.Adjust(115, 10); got != 120 {
		t.Errorf("Adjust(115, 10) with MaxPercent=120 = %v, want 120", got)
	}
}
