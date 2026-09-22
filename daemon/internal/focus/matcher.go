package focus

import (
	"strings"
	"unicode"
)

// genericTrailingSegments is the allow-list a hyphenated token's
// trailing segment must appear in before it's stripped as a generic
// suffix (rule 4 of NameCandidates' doc comment) — e.g. "brave-browser"
// -> "brave". This is deliberately an allow-list, not a general "strip
// everything after the last dash" rule: that would turn
// "jetbrains-idea" into "jetbrains" and collide across unrelated
// products. Grown one verified real-world application at a time; see
// specs/milestones/M06-focus-tracking.md.
var genericTrailingSegments = map[string]bool{
	"browser": true,
	"bin":     true,
	"desktop": true,
	"stable":  true,
	"beta":    true,
	"nightly": true,
	"dev":     true,
}

// vendorPrefixes is the allow-list for rule 5: a leading vendor tag
// stripped from a hyphenated resourceClass/desktopFileId, e.g.
// "google-chrome" -> "chrome". Also grown empirically, not derived from
// any general rule.
var vendorPrefixes = []string{"google-", "org-", "com-"}

// NameCandidates returns the identity tokens derived from
// ResourceClass, DesktopFileID, and Binary (when a real Provider filled
// it in), deduplicated case-insensitively. Caption is deliberately
// excluded — see AppInfo's doc comment.
//
// Beyond the raw field values, this applies a small set of empirical
// normalization rules, each conditional and independent:
//   - a DesktopFileID's ".desktop" suffix is stripped
//     ("org.kde.dolphin.desktop" -> "org.kde.dolphin")
//   - a token with at least two dots contributes its last dot segment,
//     the reverse-DNS-style rightmost component
//     ("org.kde.dolphin" -> "dolphin")
//   - a token with a trailing "-<segment>" contributes its prefix, but
//     ONLY when that segment is in genericTrailingSegments
//     ("brave-browser" -> "brave"; "jetbrains-idea" stays as itself,
//     since "idea" is not a generic suffix)
//   - a token with a leading vendor tag in vendorPrefixes contributes
//     the remainder ("google-chrome" -> "chrome")
//
// These are allow-lists, not a general algorithm, precisely so a rule
// doesn't overreach into a false collision across unrelated apps. The
// resulting tokens are fed directly into a model.AppMatcher's
// Binaries/AppNames/NodeNames/DesktopIDs by audio.ResolveFocused and
// the knob.assign_focused_app handler; every one of those fields
// matches by case-insensitive EXACT equality, never substring, which
// is what makes a broad candidate set safe rather than a
// false-positive risk.
func (a AppInfo) NameCandidates() []string {
	var tokens []string
	add := func(s string) {
		if s != "" {
			tokens = append(tokens, s)
		}
	}

	add(a.ResourceClass)
	add(a.DesktopFileID)
	add(a.Binary)
	if id := strings.TrimSuffix(a.DesktopFileID, ".desktop"); id != a.DesktopFileID {
		add(id)
	}

	// Apply the normalization rules once over every token gathered so
	// far -- not recursively over their own outputs. One level covers
	// every case observed in practice and keeps the candidate set from
	// growing unboundedly.
	base := append([]string(nil), tokens...)
	for _, tok := range base {
		add(lastDotSegment(tok))
		add(trimGenericSuffix(tok))
		add(trimVendorPrefix(tok))
	}

	return dedupFold(tokens)
}

// DisplayName returns a human-facing name for the application, for a
// generated AppMatcher's DisplayName. Never derived from Caption (see
// AppInfo's doc comment). Falls back to ResourceClass, then
// DesktopFileID, then "".
func (a AppInfo) DisplayName() string {
	var candidates []string
	consider := func(tok string) {
		if tok == "" {
			return
		}
		if t := trimGenericSuffix(tok); t != "" {
			candidates = append(candidates, t)
		}
		if t := trimVendorPrefix(tok); t != "" {
			candidates = append(candidates, t)
		}
	}
	consider(a.ResourceClass)
	consider(strings.TrimSuffix(a.DesktopFileID, ".desktop"))

	var name string
	switch {
	case len(candidates) > 0:
		name = shortestString(candidates)
	case a.ResourceClass != "":
		name = a.ResourceClass
	case a.DesktopFileID != "":
		name = strings.TrimSuffix(a.DesktopFileID, ".desktop")
	}
	return titleCaseFirst(name)
}

func lastDotSegment(tok string) string {
	if strings.Count(tok, ".") < 2 {
		return ""
	}
	idx := strings.LastIndex(tok, ".")
	return tok[idx+1:]
}

func trimGenericSuffix(tok string) string {
	idx := strings.LastIndex(tok, "-")
	if idx <= 0 || idx == len(tok)-1 {
		return ""
	}
	if !genericTrailingSegments[strings.ToLower(tok[idx+1:])] {
		return ""
	}
	return tok[:idx]
}

func trimVendorPrefix(tok string) string {
	lower := strings.ToLower(tok)
	for _, prefix := range vendorPrefixes {
		if strings.HasPrefix(lower, prefix) && len(tok) > len(prefix) {
			return tok[len(prefix):]
		}
	}
	return ""
}

// dedupFold removes case-insensitive duplicates, keeping the
// first-seen spelling and order.
func dedupFold(tokens []string) []string {
	seen := make(map[string]bool, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	return out
}

func shortestString(tokens []string) string {
	best := tokens[0]
	for _, t := range tokens[1:] {
		if len(t) < len(best) {
			best = t
		}
	}
	return best
}

func titleCaseFirst(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
