package engine

import (
	"context"

	"github.com/njeske/knobd/internal/actions"
	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/model"
)

// dispatchQueueDepth bounds the work channel between the run goroutine
// and the dispatcher. It exists purely so a temporarily-stuck dispatcher
// (a wedged PipeWire connection) has somewhere to absorb a burst of
// input without the run goroutine ever blocking on a send -- once full,
// new work is dropped (see Engine.enqueueDispatch), not queued
// indefinitely.
const dispatchQueueDepth = 64

// work is one item on the dispatch queue: either an Invocation to
// execute, or a request to re-enumerate the audio graph.
type work struct {
	inv    *actions.Invocation
	resync bool
}

// streamsResult is the dispatcher's answer to a resync request, fed back
// to the run goroutine so it -- and only it -- ever mutates the
// resolver's cache.
type streamsResult struct {
	sinks   []audio.Device
	sources []audio.Device
	streams []audio.Stream
}

// runDispatcher is the single goroutine that ever calls
// audio.Backend.SetVolume/SetMute/Sinks/Sources/Streams or
// actions.Registry.Execute. Serializing every one of those calls through
// one goroutine is what gives audio.Backend the read-modify-write
// atomicity its own doc comment says it does not otherwise provide (see
// specs/milestones/M04-mapping-engine-daemon.md's Architecture section).
// It serializes across every Ref, which is stronger than strictly
// required -- a per-Ref worker pool keyed on the same work type would
// relax that -- but is far simpler and correct by construction; revisit
// only if a slow target is observed to head-of-line block unrelated
// ones in practice.
//
// runDispatcher returns when in is closed, after finishing whatever it
// was already processing.
func (e *Engine) runDispatcher(ctx context.Context, in <-chan work, out chan<- streamsResult) {
	for w, ok := <-in; ok; w, ok = <-in {
		for _, item := range coalesce(drain(in, w)) {
			e.executeWork(ctx, item, out)
		}
	}
}

// drain collects first plus every item already queued on in, without
// blocking -- the "drain-and-merge" half of coalescing described in
// coalesce's doc comment.
func drain(in <-chan work, first work) []work {
	batch := []work{first}
	for {
		select {
		case w, ok := <-in:
			if !ok {
				return batch
			}
			batch = append(batch, w)
		default:
			return batch
		}
	}
}

// coalesce merges adjacent (immediately consecutive in batch) work items
// that share an action type, triggering control, and resolved refs:
//   - volume.adjust: sum the Deltas. Safe even with a non-linear curve:
//     audio.Curve.Adjust adds stepPercent/100 in normalized position
//     space, so Adjust(cur, step*N) equals N sequential single-step
//     applications -- they differ only when the sequence would have
//     clamped at 0/MaxPercent partway through, and clamping once at the
//     end (the merged form) is the more correct of the two anyway.
//   - volume.follow: keep only the last Value, discarding the rest. This
//     is what makes the fader usable -- the reference doc recorded 404
//     pitch-bend messages in a single free-play session.
//
// A resync request is never merged with anything; every other action
// type is dispatched as-is, once per firing.
func coalesce(batch []work) []work {
	out := make([]work, 0, len(batch))
	for _, w := range batch {
		if w.resync || w.inv == nil {
			out = append(out, w)
			continue
		}
		if n := len(out); n > 0 && mergeable(out[n-1], w) {
			out[n-1] = merge(out[n-1], w)
			continue
		}
		out = append(out, w)
	}
	return out
}

func mergeable(a, b work) bool {
	if a.resync || b.resync || a.inv == nil || b.inv == nil {
		return false
	}
	if a.inv.Action.ActionType() != b.inv.Action.ActionType() {
		return false
	}
	switch a.inv.Action.ActionType() {
	case model.ActionVolumeAdjust, model.ActionVolumeFollow:
		// only these two types have a defined merge rule below
	default:
		return false
	}
	if a.inv.Control != b.inv.Control {
		return false
	}
	return refsEqual(a.inv.Refs, b.inv.Refs)
}

func merge(a, b work) work {
	merged := *a.inv
	switch a.inv.Action.ActionType() {
	case model.ActionVolumeAdjust:
		merged.Delta += b.inv.Delta
	case model.ActionVolumeFollow:
		merged.Value = b.inv.Value
	}
	merged.At = b.inv.At
	return work{inv: &merged}
}

func refsEqual(a, b []audio.Ref) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (e *Engine) executeWork(ctx context.Context, w work, out chan<- streamsResult) {
	if w.resync {
		e.executeResync(ctx, out)
		return
	}
	if err := e.deps.Registry.Execute(ctx, *w.inv); err != nil {
		e.log.Error("engine: action execution failed",
			"control", w.inv.Control, "gesture", w.inv.Gesture,
			"actionType", w.inv.Action.ActionType(), "err", err)
	}
}

func (e *Engine) executeResync(ctx context.Context, out chan<- streamsResult) {
	sinks, err := e.deps.Audio.Sinks(ctx)
	if err != nil {
		e.log.Warn("engine: resync: Sinks failed", "err", err)
		return
	}
	sources, err := e.deps.Audio.Sources(ctx)
	if err != nil {
		e.log.Warn("engine: resync: Sources failed", "err", err)
		return
	}
	streams, err := e.deps.Audio.Streams(ctx)
	if err != nil {
		e.log.Warn("engine: resync: Streams failed", "err", err)
		return
	}
	select {
	case out <- streamsResult{sinks: sinks, sources: sources, streams: streams}:
	case <-ctx.Done():
	}
}

// enqueueDispatch sends w to dispatchCh without ever blocking the run
// goroutine: if the queue is full, the item is dropped with a log rather
// than stalling gesture timing behind a wedged PipeWire connection.
func (e *Engine) enqueueDispatch(dispatchCh chan<- work, w work, what string) {
	select {
	case dispatchCh <- w:
	default:
		e.log.Warn("engine: dispatch queue full; dropping", "what", what)
	}
}
