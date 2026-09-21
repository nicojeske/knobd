package audio

import "math"

// DefaultMaxPercent is the ceiling Curve.Adjust and the real backend's
// SetVolume clamp to when Curve.MaxPercent/Options.MaxPercent is unset.
// Roughly PulseAudio's own UI ceiling (PA_VOLUME_UI_MAX is ~153%, +11dB)
// and KDE's opt-in maximum; anything above 100% is software gain and
// will clip depending on the source material.
const DefaultMaxPercent = 150

// Curve maps a VolumeAdjustAction's StepPercent onto an actual change in
// volume percent. See specs/milestones/M03-audio-control.md's Design
// section for why the default (Exponent 1) is linear rather than the
// cubic originally guessed there: KDE's and pavucontrol's sliders are
// already linear in PulseAudio's percent scale — the perceptual cubic
// curve is baked into the percent-to-gain relationship the backend
// applies (see the "not Volume.Linear(), despite the name" comment in
// pulse.go) — so Exponent 1 is what actually matches them, and
// VolumeAdjustAction.CurveExponent remains available per-binding for
// anyone who wants an old-school fader feel instead.
//
// With Exponent != 1, StepPercent no longer means "percentage points of
// volume" — it means percent of knob travel in the curve's normalized
// position space. The two coincide only at Exponent == 1 (the default),
// which is worth remembering since the field is still named StepPercent.
type Curve struct {
	// Exponent is γ. <= 0 is treated as 1 (VolumeAdjustAction's doc
	// comment says CurveExponent: 0 means "use the engine default").
	// Clamped to [0.1, 10] to keep both extremes usable: much outside
	// that range the curve is either indistinguishable from a hard
	// on/off (very large γ) or dead for the first dozen detents (very
	// small γ, the mirror image of the large-γ problem at the other
	// end).
	Exponent float64
	// MaxPercent is the ceiling Adjust clamps to. <= 0 means
	// DefaultMaxPercent.
	MaxPercent float64
}

func (c Curve) normalize() (gamma, max float64) {
	gamma = c.Exponent
	if gamma <= 0 {
		gamma = 1
	}
	if gamma < 0.1 {
		gamma = 0.1
	}
	if gamma > 10 {
		gamma = 10
	}
	max = c.MaxPercent
	if max <= 0 {
		max = DefaultMaxPercent
	}
	return gamma, max
}

// Adjust returns the volume percent that results from stepping current
// by stepPercent along the curve, clamped to [0, MaxPercent]. current is
// clamped to >= 0 before any exponent math — a negative input would
// otherwise produce NaN (math.Pow of a negative base to a fractional
// exponent), which would go on to silently set the volume to
// proto.VolumeMax by the time it reaches NormVolume(percent/100).
func (c Curve) Adjust(current, stepPercent float64) float64 {
	gamma, max := c.normalize()
	if current < 0 || math.IsNaN(current) {
		current = 0
	}

	var result float64
	if gamma == 1 {
		// math.Pow(x, 1) == x per its own doc, but this is also just
		// clearer to read as the identity case it is.
		result = current + stepPercent
	} else {
		pos := math.Pow(current/100, 1/gamma)
		pos += stepPercent / 100
		if pos < 0 {
			pos = 0
		}
		result = 100 * math.Pow(pos, gamma)
	}

	if math.IsNaN(result) {
		return 0
	}
	if result < 0 {
		return 0
	}
	if result > max {
		return max
	}
	return result
}
