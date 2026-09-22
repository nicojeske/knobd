package audio

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/njeske/knobd/internal/model"
)

// propProcessID is the PipeWire property ResolveFocused reads to get a
// stream's owning pid, for the rung-3 process-tree fallback below. It
// is read directly (not through matcher.go's propBinaryReal/etc., which
// are all string identity properties Resolve compares by equality) —
// this one gets parsed as an integer.
const propProcessID = "application.process.id"

// FocusRequest is a focused window reduced to the evidence
// ResolveFocused can act on. This package deliberately does not import
// daemon/internal/focus (that dependency would run backwards: audio is
// the PipeWire-facing package, focus is the window-manager-facing one)
// so a caller — daemon/internal/engine's resolver — builds this from a
// focus.AppInfo itself.
type FocusRequest struct {
	// PID is the focused window's own pid, not necessarily the pid that
	// owns its audio stream (a browser's window pid is commonly a
	// parent of the process that actually publishes audio — see
	// specs/milestones/M06-focus-tracking.md).
	PID int
	// Names are identity tokens for the focused application, from
	// focus.AppInfo.NameCandidates() (which already folds in
	// ResourceClass, DesktopFileID, and the /proc/<pid>/exe-derived
	// Binary).
	Names []string
}

// ProcLookup is the slice of daemon/internal/proctree.Walker
// ResolveFocused's rung 3 needs, defined at its point of use so this
// package doesn't depend on proctree either (the same reasoning as
// FocusRequest's doc comment: a plain interface here keeps audio
// testable with a fake, in-package double, with no real /proc
// involved). A nil ProcLookup disables rung 3 entirely — names-only
// matching, rungs 1 continues to work.
type ProcLookup interface {
	IsRelated(a, b, maxDepth int) bool
}

// procMaxDepth bounds rung 3's ancestry walk. Matches
// proctree.DefaultMaxDepth without importing that package for a single
// constant.
const procMaxDepth = 16

// ResolveFocused returns every Stream belonging to the focused window
// req describes, by a three-rung ladder, most-evidence-first:
//
//  1. Name-based matching: req.Names (already covering
//     ResourceClass/DesktopFileID and their normalized forms) matched
//     against a Stream's Binaries/AppNames/NodeNames/DesktopIDs, via
//     the same case-insensitive exact-equality rules as Resolve. This
//     alone handles the general case.
//  2. (folded into rung 1) req.Names includes the focused window's
//     /proc/<pid>/exe-derived binary name when the caller filled it in
//     — this is what makes a browser like Brave resolve correctly even
//     though its window's resourceClass ("brave-browser") differs from
//     its stream's application.id (which it doesn't even publish): see
//     testdata/pipewire/focus-brave-pid-mismatch.json.
//  3. Process-tree matching (pl, rung 3): for each stream with a
//     parseable application.process.id, ask pl.IsRelated(streamPID,
//     req.PID, ...) — true if one is an ancestor of the other. This
//     ONLY runs when rungs 1-2 matched nothing at all, never unioned
//     with them: under Flatpak/bubblewrap, application.process.id is a
//     pid in the app's own sandboxed namespace (the same sharp edge
//     AnnotateProcessBinaries already documents), so an ancestry check
//     can walk an unrelated host process and match the WRONG
//     application's stream — a failure mode much worse than rungs 1-2
//     simply matching nothing. Name evidence is trusted first, process
//     ancestry only as a last resort.
//
// Unlike Resolve, this is not a pure function when pl is non-nil: rung
// 3 reads /proc through it. Results are deduplicated by Stream.ID and
// sorted by ID, matching pulseBackend.Streams' order.
//
// viaProcessTree reports whether rung 3 is what produced the result
// (false whenever rungs 1-2 matched, or nothing matched at all) —
// ResolveFocused has no logger of its own, so it hands this back for
// the caller (engine's resolver, which does) to log at Debug. That log
// line is the one piece of live evidence that tells the difference,
// during manual verification against Brave, between "rung 2's exe
// lookup carried it" and "rung 3's ancestry walk carried it".
func ResolveFocused(req FocusRequest, streams []Stream, pl ProcLookup) (matched []Stream, viaProcessTree bool, err error) {
	if len(req.Names) > 0 {
		matcher := model.AppMatcher{
			ID:         "focused",
			Binaries:   req.Names,
			AppNames:   req.Names,
			NodeNames:  req.Names,
			DesktopIDs: req.Names,
		}
		byName, err := resolve(matcher, streams)
		if err != nil {
			return nil, false, fmt.Errorf("audio: resolve focused (names): %w", err)
		}
		if len(byName) > 0 {
			return dedupSortStreams(byName), false, nil
		}
	}

	if pl == nil || req.PID <= 0 {
		return nil, false, nil
	}

	var byProcTree []Stream
	for _, s := range streams {
		pidStr := s.Props[propProcessID]
		if pidStr == "" {
			continue
		}
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid <= 0 {
			continue
		}
		if pl.IsRelated(pid, req.PID, procMaxDepth) {
			byProcTree = append(byProcTree, s)
		}
	}
	if len(byProcTree) == 0 {
		return nil, false, nil
	}
	return dedupSortStreams(byProcTree), true, nil
}

func dedupSortStreams(streams []Stream) []Stream {
	seen := make(map[string]bool, len(streams))
	out := make([]Stream, 0, len(streams))
	for _, s := range streams {
		if seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
