package audio

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jfreymuth/pulse/proto"
)

// heartbeatInterval bounds how long a truly silent connection failure
// (readLoop's own ConnectionClosed callback fires only on io.EOF — an
// ECONNRESET or a protocol parse error just exits the loop with no
// callback at all) can go unnoticed.
const heartbeatInterval = 30 * time.Second

// eventBufferSize is the internal handoff channel's capacity between
// proto.Client.Callback (which must never block — see dispatcher's doc
// comment) and dispatcher.run.
const eventBufferSize = 64

// subBufferSize is each external Subscribe caller's channel capacity.
const subBufferSize = 32

// dispatcher owns Subscribe's fan-out to external callers and the
// mandatory Callback -> channel handoff. proto.Client.readLoop invokes
// Callback inline and does not resume reading until it returns, so a
// Request call from inside Callback can never see its own reply — it
// would stall readLoop for the full request timeout and then fail. Every
// method here that Callback invokes directly (onSubscribeEvent,
// onConnectionClosed) is therefore non-blocking and does no I/O; the
// round-trip GetSinkInputInfo/GetSourceOutputInfo/GetSinkInfo/
// GetSourceInfo calls needed to translate an event happen only in run,
// a separate goroutine reading from the handoff channel.
type dispatcher struct {
	req    requester
	logger *slog.Logger
	// heartbeat is heartbeatInterval by default; tests shrink it so a
	// simulated dead connection doesn't take 30s to notice.
	heartbeat time.Duration

	events  chan *proto.SubscribeEvent
	dropped atomic.Uint64

	mu        sync.Mutex
	subs      map[*audioSub]struct{}
	lastKnown map[Ref]Stream // for populating a removed stream's Props
	closed    bool
	done      chan struct{}
}

type audioSub struct{ ch chan Event }

func newDispatcher(req requester, logger *slog.Logger) *dispatcher {
	return &dispatcher{
		req:       req,
		logger:    logger,
		heartbeat: heartbeatInterval,
		events:    make(chan *proto.SubscribeEvent, eventBufferSize),
		subs:      make(map[*audioSub]struct{}),
		lastKnown: make(map[Ref]Stream),
		done:      make(chan struct{}),
	}
}

// start issues the single, per-connection Subscribe request (issuing it
// again would just replace the mask, not add a second subscription) and
// launches run.
func (d *dispatcher) start() error {
	if err := d.req.Request(&proto.Subscribe{Mask: subscribeMask}, nil); err != nil {
		return err
	}
	go d.run()
	return nil
}

// onSubscribeEvent is called directly from proto.Client.Callback. See
// the type doc comment for why it must not block or do I/O: a full
// buffer means run has fallen behind, so the event is dropped and
// counted rather than risking a backend-wide deadlock.
func (d *dispatcher) onSubscribeEvent(ev *proto.SubscribeEvent) {
	select {
	case d.events <- ev:
	default:
		d.dropped.Add(1)
	}
}

// onConnectionClosed is called directly from proto.Client.Callback on
// io.EOF. It's one of three ways run notices the connection died — see
// run's heartbeat case for the other two (a non-proto.Error Request
// failure, or no response at all to a periodic GetServerInfo).
func (d *dispatcher) onConnectionClosed() {
	d.markDone()
}

func (d *dispatcher) markDone() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.closed {
		d.closed = true
		close(d.done)
	}
}

func (d *dispatcher) run() {
	heartbeat := time.NewTicker(d.heartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case ev := <-d.events:
			d.handleEvent(ev)
			if n := d.dropped.Swap(0); n > 0 {
				d.logger.Warn("audio: dropped subscribe events while a subscriber was behind, resyncing", "count", n)
				d.fanOut(Event{Kind: EventResync})
			}
		case <-heartbeat.C:
			var info proto.GetServerInfoReply
			if err := d.req.Request(&proto.GetServerInfo{}, &info); err != nil {
				var protoErr proto.Error
				if !errors.As(err, &protoErr) {
					// Not a protocol-level error (like ErrNoSuchEntity)
					// — the connection itself is dead, and readLoop
					// won't always tell us (see onConnectionClosed).
					d.logger.Warn("audio: heartbeat failed, treating connection as lost", "err", err)
					d.markDone()
				}
			}
		case <-d.done:
			d.closeAllSubs()
			return
		}
	}
}

func (d *dispatcher) handleEvent(ev *proto.SubscribeEvent) {
	facility := ev.Event.GetFacility()
	typ := ev.Event.GetType()
	switch facility {
	case proto.EventServer:
		d.fanOut(Event{Kind: EventDefaultChanged})
	case proto.EventSink:
		d.handleDeviceEvent(RefSink, ev.Index, typ)
	case proto.EventSource:
		d.handleDeviceEvent(RefSource, ev.Index, typ)
	case proto.EventSinkSinkInput:
		d.handleStreamEvent(RefStream, ev.Index, typ)
	case proto.EventSinkSourceOutput:
		d.handleStreamEvent(RefRecord, ev.Index, typ)
	}
}

func (d *dispatcher) handleStreamEvent(kind RefKind, idx uint32, typ proto.SubscriptionEventType) {
	ref := Ref{Kind: kind, ID: strconv.FormatUint(uint64(idx), 10)}

	if typ == proto.EventRemove {
		d.mu.Lock()
		last, ok := d.lastKnown[ref]
		delete(d.lastKnown, ref)
		d.mu.Unlock()
		if !ok {
			last = Stream{ID: ref.ID, Direction: streamDirectionFor(kind)}
		}
		d.fanOut(Event{Kind: EventStreamRemoved, Stream: &last})
		return
	}

	stream, state, err := d.fetchStream(kind, idx)
	if err != nil {
		var protoErr proto.Error
		if errors.As(err, &protoErr) && protoErr == proto.ErrNoSuchEntity {
			// The stream died between the event firing and our fetch;
			// not an error, just nothing left to report.
			return
		}
		d.logger.Debug("audio: fetching stream info after subscribe event failed", "ref", ref, "err", err)
		return
	}
	d.mu.Lock()
	d.lastKnown[ref] = stream
	d.mu.Unlock()
	d.fanOut(Event{Kind: EventStreamChanged, Stream: &stream, State: &state})
}

func streamDirectionFor(kind RefKind) StreamDirection {
	if kind == RefRecord {
		return StreamRecord
	}
	return StreamPlayback
}

func (d *dispatcher) fetchStream(kind RefKind, idx uint32) (Stream, VolumeState, error) {
	switch kind {
	case RefStream:
		var info proto.GetSinkInputInfoReply
		if err := d.req.Request(&proto.GetSinkInputInfo{SinkInputIndex: idx}, &info); err != nil {
			return Stream{}, VolumeState{}, err
		}
		stream := sinkInputToStream(&info)
		state, _ := channelVolumesToState(info.ChannelVolumes)
		state.Muted = info.Muted
		return stream, state, nil
	case RefRecord:
		var info proto.GetSourceOutputInfoReply
		if err := d.req.Request(&proto.GetSourceOutputInfo{SourceOutpuIndex: idx}, &info); err != nil {
			return Stream{}, VolumeState{}, err
		}
		stream := sourceOutputToStream(&info)
		state, _ := channelVolumesToState(info.ChannelVolumes)
		state.Muted = info.Muted
		return stream, state, nil
	default:
		return Stream{}, VolumeState{}, errors.New("audio: fetchStream called with a non-stream RefKind")
	}
}

func (d *dispatcher) handleDeviceEvent(kind RefKind, idx uint32, typ proto.SubscriptionEventType) {
	if typ == proto.EventRemove {
		d.fanOut(Event{Kind: EventDeviceRemoved, Device: &Device{ID: strconv.FormatUint(uint64(idx), 10)}})
		return
	}

	var device Device
	switch kind {
	case RefSink:
		var info proto.GetSinkInfoReply
		if err := d.req.Request(&proto.GetSinkInfo{SinkIndex: idx, SinkName: ""}, &info); err != nil {
			d.logger.Debug("audio: GetSinkInfo after subscribe event failed", "index", idx, "err", err)
			return
		}
		device = Device{ID: info.SinkName, Description: sinkDescription(&info)}
	case RefSource:
		var info proto.GetSourceInfoReply
		if err := d.req.Request(&proto.GetSourceInfo{SourceIndex: idx, SourceName: ""}, &info); err != nil {
			d.logger.Debug("audio: GetSourceInfo after subscribe event failed", "index", idx, "err", err)
			return
		}
		device = Device{ID: info.SourceName, Description: sourceDescription(&info)}
	default:
		return
	}
	// IsDefault is deliberately left false here: computing it needs a
	// second round-trip (GetServerInfo) on every single device event,
	// and EventDefaultChanged (fired from the Server facility) already
	// tells callers when it's worth re-calling Sinks/Sources for the
	// authoritative answer.
	d.fanOut(Event{Kind: EventDeviceChanged, Device: &device})
}

// fanOut sends ev to every subscriber, non-blocking: a slow subscriber
// must never stall this loop, which would backpressure d.events, which
// would block onSubscribeEvent's Callback, which would stall
// proto.Client's readLoop and starve every pending Request across the
// whole backend — see the type doc comment.
func (d *dispatcher) fanOut(ev Event) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for sub := range d.subs {
		select {
		case sub.ch <- ev:
		default:
			d.logger.Debug("audio: dropped event for a slow subscriber", "kind", ev.Kind)
		}
	}
}

// subscribe registers a new external Subscribe(ctx) caller. Membership
// in d.subs and closing a subscriber's channel are both always done
// under d.mu — by fanOut, by this method's own ctx-watch goroutine, and
// by closeAllSubs — with a "still present?" check immediately before
// closing, so exactly one of them ever calls close(sub.ch) for a given
// subscriber, however Close/ctx-cancellation/backend-death race.
func (d *dispatcher) subscribe(ctx context.Context) chan Event {
	sub := &audioSub{ch: make(chan Event, subBufferSize)}

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		close(sub.ch)
		return sub.ch
	}
	d.subs[sub] = struct{}{}
	d.mu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
		case <-d.done:
			return // closeAllSubs will close sub.ch itself
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		if _, ok := d.subs[sub]; ok {
			delete(d.subs, sub)
			close(sub.ch)
		}
	}()

	return sub.ch
}

func (d *dispatcher) closeAllSubs() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for sub := range d.subs {
		delete(d.subs, sub)
		close(sub.ch)
	}
}

// stop shuts the dispatcher down, closing every live subscriber channel.
// Idempotent and safe to call alongside a concurrent onConnectionClosed
// or heartbeat failure — both funnel through the same markDone.
func (d *dispatcher) stop() {
	d.markDone()
}
