package audio

import (
	"testing"
)

const braveFixturePath = "../../../testdata/pipewire/focus-brave-pid-mismatch.json"

// fakeProcLookup is an in-memory ProcLookup double: pairs lists every
// (a, b) considered related, with no /proc access at all.
type fakeProcLookup struct {
	related map[[2]int]bool
	calls   int
}

func newFakeProcLookup(pairs ...[2]int) *fakeProcLookup {
	f := &fakeProcLookup{related: make(map[[2]int]bool)}
	for _, p := range pairs {
		f.related[p] = true
		f.related[[2]int{p[1], p[0]}] = true // IsRelated is symmetric
	}
	return f
}

func (f *fakeProcLookup) IsRelated(a, b, maxDepth int) bool {
	f.calls++
	return f.related[[2]int{a, b}]
}

func TestResolveFocusedNamesOnly(t *testing.T) {
	streams := loadFixtureStreamsFrom(t, braveFixturePath)

	got, viaProcTree, err := ResolveFocused(FocusRequest{Names: []string{"brave"}}, streams, nil)
	if err != nil {
		t.Fatalf("ResolveFocused: %v", err)
	}
	if viaProcTree {
		t.Fatal("expected a name match, not a process-tree match")
	}
	if len(got) != 1 || got[0].ID != "154" {
		t.Fatalf("ResolveFocused names-only = %v, want just stream 154", streamIDs(got))
	}
}

func TestResolveFocusedProcessTreeAloneFindsBrave(t *testing.T) {
	streams := loadFixtureStreamsFrom(t, braveFixturePath)
	// Window pid 3172 is an ancestor of the Brave stream's pid (4417),
	// per the real ancestry confirmed live on the dev machine. No name
	// evidence at all, so only rung 3 can find it.
	pl := newFakeProcLookup([2]int{4417, 3172})

	got, viaProcTree, err := ResolveFocused(FocusRequest{PID: 3172}, streams, pl)
	if err != nil {
		t.Fatalf("ResolveFocused: %v", err)
	}
	if !viaProcTree {
		t.Fatal("expected the process-tree rung to have produced this result")
	}
	if len(got) != 1 || got[0].ID != "154" {
		t.Fatalf("ResolveFocused process-tree-only = %v, want just stream 154", streamIDs(got))
	}
}

func TestResolveFocusedProcessTreeReverseDirection(t *testing.T) {
	streams := loadFixtureStreamsFrom(t, braveFixturePath)
	// The rarer direction: the STREAM's pid (4417) is an ancestor of
	// the WINDOW's pid (9999, a made-up descendant) rather than the
	// other way around. IsRelated is documented and implemented
	// symmetrically, so ResolveFocused must find this too.
	pl := newFakeProcLookup([2]int{4417, 9999})

	got, viaProcTree, err := ResolveFocused(FocusRequest{PID: 9999}, streams, pl)
	if err != nil {
		t.Fatalf("ResolveFocused: %v", err)
	}
	if !viaProcTree {
		t.Fatal("expected the process-tree rung to have produced this result")
	}
	if len(got) != 1 || got[0].ID != "154" {
		t.Fatalf("ResolveFocused reverse process-tree = %v, want just stream 154", streamIDs(got))
	}
}

func TestResolveFocusedProcessTreeDoesNotRunWhenNamesMatched(t *testing.T) {
	streams := loadFixtureStreamsFrom(t, braveFixturePath)
	// Poison the process-tree lookup so it would over-match (claim
	// EVERYTHING is related) if it were ever consulted. If rung 3 fired
	// here, this test would see stream 200 (vesktop) in the result too.
	pl := &alwaysRelatedProcLookup{}

	got, viaProcTree, err := ResolveFocused(FocusRequest{PID: 3172, Names: []string{"brave"}}, streams, pl)
	if err != nil {
		t.Fatalf("ResolveFocused: %v", err)
	}
	if viaProcTree {
		t.Fatal("rung 3 must not run when rungs 1-2 already matched")
	}
	if pl.calls != 0 {
		t.Fatalf("ProcLookup.IsRelated was called %d times, want 0 (rung 3 should be skipped entirely)", pl.calls)
	}
	if len(got) != 1 || got[0].ID != "154" {
		t.Fatalf("ResolveFocused = %v, want just stream 154 (not the poisoned rung-3 match)", streamIDs(got))
	}
}

type alwaysRelatedProcLookup struct{ calls int }

func (a *alwaysRelatedProcLookup) IsRelated(x, y, maxDepth int) bool {
	a.calls++
	return true
}

func TestResolveFocusedNoMatch(t *testing.T) {
	streams := loadFixtureStreamsFrom(t, braveFixturePath)
	pl := newFakeProcLookup() // no relations at all

	got, viaProcTree, err := ResolveFocused(FocusRequest{PID: 9999, Names: []string{"nonexistent-app"}}, streams, pl)
	if err != nil {
		t.Fatalf("ResolveFocused: %v", err)
	}
	if viaProcTree {
		t.Fatal("expected no match at all, not a process-tree match")
	}
	if len(got) != 0 {
		t.Fatalf("ResolveFocused = %v, want none", streamIDs(got))
	}
}

func TestResolveFocusedNilProcLookupDisablesRung3(t *testing.T) {
	streams := loadFixtureStreamsFrom(t, braveFixturePath)

	got, viaProcTree, err := ResolveFocused(FocusRequest{PID: 3172}, streams, nil)
	if err != nil {
		t.Fatalf("ResolveFocused: %v", err)
	}
	if viaProcTree {
		t.Fatal("a nil ProcLookup must never report a process-tree match")
	}
	if len(got) != 0 {
		t.Fatalf("ResolveFocused with nil ProcLookup and no name evidence = %v, want none", streamIDs(got))
	}
}

func TestResolveFocusedStreamWithNonNumericPIDIsSkipped(t *testing.T) {
	streams := []Stream{
		{ID: "1", Direction: StreamPlayback, Props: map[string]string{propProcessID: "not-a-number"}},
		{ID: "2", Direction: StreamPlayback, Props: map[string]string{}}, // no pid property at all
	}
	pl := &alwaysRelatedProcLookup{}

	got, viaProcTree, err := ResolveFocused(FocusRequest{PID: 42}, streams, pl)
	if err != nil {
		t.Fatalf("ResolveFocused: %v", err)
	}
	if viaProcTree || len(got) != 0 {
		t.Fatalf("ResolveFocused = %v (viaProcTree=%v), want none (both streams lack a usable pid)", streamIDs(got), viaProcTree)
	}
}

func TestResolveFocusedDedupAndSortOrder(t *testing.T) {
	// A stream matching by both name AND (hypothetically) process tree
	// must appear exactly once, and results are sorted by ID.
	streams := []Stream{
		{ID: "300", Direction: StreamPlayback, Props: map[string]string{"application.name": "brave", propProcessID: "10"}},
		{ID: "100", Direction: StreamPlayback, Props: map[string]string{"application.name": "brave", propProcessID: "20"}},
	}
	got, _, err := ResolveFocused(FocusRequest{Names: []string{"brave"}}, streams, nil)
	if err != nil {
		t.Fatalf("ResolveFocused: %v", err)
	}
	if len(got) != 2 || got[0].ID != "100" || got[1].ID != "300" {
		t.Fatalf("ResolveFocused = %v, want [100 300] (sorted by ID)", streamIDs(got))
	}
}
