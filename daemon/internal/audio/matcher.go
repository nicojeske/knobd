package audio

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/njeske/knobd/internal/model"
)

// Property keys Resolve reads from Stream.Props. These mirror what
// PipeWire (via the pipewire-pulse compatibility layer) actually
// publishes; see testdata/pipewire/README.md for a survey of which keys
// are and aren't reliably present.
const (
	propAppName     = "application.name"
	propNodeName    = "node.name"
	propDesktopID   = "application.id"
	propMediaName   = "media.name"
	propBinaryReal  = "application.process.binary"
	propBinaryKnobd = "knobd.process.binary" // see AnnotateProcessBinaries
)

// mediaNameRxCache memoizes compiled MediaNameRx patterns, keyed by the
// pattern string. Resolve runs once per detent per stream in the
// mapping engine's hot path (M04); recompiling a regexp on every call
// would be wasteful, and the patterns come from a small, mostly-static
// set of configured AppMatchers.
var mediaNameRxCache sync.Map // string -> *regexp.Regexp

func compileMediaNameRx(pattern string) (*regexp.Regexp, error) {
	if v, ok := mediaNameRxCache.Load(pattern); ok {
		return v.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	mediaNameRxCache.Store(pattern, re)
	return re, nil
}

// resolve implements Resolve. See model.AppMatcher's doc comment for the
// matching rules: OR across and within every populated field. Binaries,
// AppNames, NodeNames, and DesktopIDs all match case-insensitively —
// pactl/pw-dump display whatever casing convention a given application
// happens to use (testdata/pipewire/pw-dump-sample.json alone has
// "Brave"/"brave", "vesktop", and "Pal"), and matchers are hand-written
// against what a user sees there. MediaNameRx stays case-sensitive
// (users can prefix a pattern with "(?i)") and is unanchored, so e.g.
// "Pal" also matches a hypothetical "Palworld" — write "^Pal$" to avoid
// that.
//
// A matcher with no populated fields matches nothing; an empty string in
// a list field never matches a stream that lacks that property (a
// missing property compares equal to "", which would otherwise make an
// accidental empty entry match every unlabeled stream).
//
// Binaries checks application.process.binary first, then
// knobd.process.binary — the synthesized property AnnotateProcessBinaries
// writes when a stream publishes a pid but not a binary name (see its
// doc comment). Streams() applies that annotation before Resolve ever
// sees the streams, so Resolve itself stays a pure function with no
// /proc access, directly testable against
// testdata/pipewire/pw-dump-sample.json with no backend at all.
func resolve(matcher model.AppMatcher, streams []Stream) ([]Stream, error) {
	var mediaRx *regexp.Regexp
	if matcher.MediaNameRx != "" {
		re, err := compileMediaNameRx(matcher.MediaNameRx)
		if err != nil {
			return nil, fmt.Errorf("audio: matcher %q has invalid mediaNameRx %q: %w", matcher.ID, matcher.MediaNameRx, err)
		}
		mediaRx = re
	}

	var out []Stream
	for _, s := range streams {
		if streamMatches(matcher, s, mediaRx) {
			out = append(out, s)
		}
	}
	return out, nil
}

func streamMatches(matcher model.AppMatcher, s Stream, mediaRx *regexp.Regexp) bool {
	if matchesAnyFold(matcher.Binaries, firstNonEmpty(s.Props[propBinaryReal], s.Props[propBinaryKnobd])) {
		return true
	}
	if matchesAnyFold(matcher.AppNames, s.Props[propAppName]) {
		return true
	}
	if matchesAnyFold(matcher.NodeNames, s.Props[propNodeName]) {
		return true
	}
	if matchesAnyFold(matcher.DesktopIDs, s.Props[propDesktopID]) {
		return true
	}
	if mediaRx != nil && mediaRx.MatchString(s.Props[propMediaName]) {
		return true
	}
	return false
}

// matchesAnyFold reports whether value case-insensitively equals any
// non-empty candidate in list. An empty value (the property was absent
// from Props) never matches, even if list happens to contain "" — an
// empty AppMatcher field entry should never accidentally match every
// stream lacking that property.
func matchesAnyFold(list []string, value string) bool {
	if value == "" {
		return false
	}
	for _, cand := range list {
		if cand != "" && strings.EqualFold(cand, value) {
			return true
		}
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
