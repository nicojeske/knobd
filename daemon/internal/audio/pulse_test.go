package audio

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jfreymuth/pulse/proto"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeRequester answers Request calls by type-switching on the concrete
// request type and delegating to a per-test handler. proto.RequestArgs'
// command() method is unexported, so a fake can't implement the request
// types themselves — but it can recognize and answer them, which is all
// pulseBackend's mapping logic needs to be unit-testable with no
// PipeWire (see requester's doc comment in pulse.go).
type fakeRequester struct {
	handle func(req proto.RequestArgs, reply proto.Reply) error
}

func (f *fakeRequester) Request(req proto.RequestArgs, reply proto.Reply) error {
	return f.handle(req, reply)
}

// --- proto.Undefined rule ---

func TestPulseBackendUsesUndefinedIndexForNameAddressedSink(t *testing.T) {
	var sawGetSinkIndex, sawSetSinkIndex uint32 = 123, 456 // deliberately wrong, must be overwritten
	fr := &fakeRequester{handle: func(req proto.RequestArgs, reply proto.Reply) error {
		switch r := req.(type) {
		case *proto.GetSinkInfo:
			sawGetSinkIndex = r.SinkIndex
			if r.SinkName != "mysink" {
				t.Errorf("GetSinkInfo.SinkName = %q, want %q", r.SinkName, "mysink")
			}
			*reply.(*proto.GetSinkInfoReply) = proto.GetSinkInfoReply{
				SinkIndex: 7, SinkName: "mysink",
				ChannelVolumes: proto.ChannelVolumes{proto.VolumeNorm, proto.VolumeNorm},
			}
			return nil
		case *proto.SetSinkVolume:
			sawSetSinkIndex = r.SinkIndex
			if r.SinkName != "mysink" {
				t.Errorf("SetSinkVolume.SinkName = %q, want %q", r.SinkName, "mysink")
			}
			return nil
		default:
			t.Fatalf("unexpected request %T", req)
			return nil
		}
	}}

	b := &pulseBackend{client: fr, opts: Options{MaxPercent: DefaultMaxPercent}}
	if err := b.SetVolume(t.Context(), Ref{Kind: RefSink, ID: "mysink"}, 50); err != nil {
		t.Fatalf("SetVolume: %v", err)
	}
	if sawGetSinkIndex != proto.Undefined {
		t.Errorf("GetSinkInfo.SinkIndex = %#x, want proto.Undefined (%#x) — leaving it at the zero value would silently address sink 0", sawGetSinkIndex, uint32(proto.Undefined))
	}
	if sawSetSinkIndex != proto.Undefined {
		t.Errorf("SetSinkVolume.SinkIndex = %#x, want proto.Undefined (%#x)", sawSetSinkIndex, uint32(proto.Undefined))
	}
}

func TestPulseBackendUsesUndefinedIndexForMute(t *testing.T) {
	fr := &fakeRequester{handle: func(req proto.RequestArgs, reply proto.Reply) error {
		switch r := req.(type) {
		case *proto.SetSourceMute:
			if r.SourceIndex != proto.Undefined {
				t.Errorf("SetSourceMute.SourceIndex = %#x, want proto.Undefined", r.SourceIndex)
			}
			if r.SourceName != "mysource" {
				t.Errorf("SetSourceMute.SourceName = %q, want mysource", r.SourceName)
			}
			return nil
		default:
			t.Fatalf("unexpected request %T", req)
			return nil
		}
	}}
	b := &pulseBackend{client: fr, opts: Options{MaxPercent: DefaultMaxPercent}}
	if err := b.SetMute(t.Context(), Ref{Kind: RefSource, ID: "mysource"}, true); err != nil {
		t.Fatalf("SetMute: %v", err)
	}
}

// --- GetVolume / VolumeWritable ---

func TestPulseBackendGetVolumeRejectsUnwritableStream(t *testing.T) {
	fr := &fakeRequester{handle: func(req proto.RequestArgs, reply proto.Reply) error {
		*reply.(*proto.GetSinkInputInfoReply) = proto.GetSinkInputInfoReply{
			SinkInputIndex: 1,
			ChannelVolumes: proto.ChannelVolumes{proto.VolumeNorm},
			VolumeWritable: false,
		}
		return nil
	}}
	b := &pulseBackend{client: fr, opts: Options{MaxPercent: DefaultMaxPercent}}
	if _, err := b.GetVolume(t.Context(), Ref{Kind: RefStream, ID: "1"}); err == nil {
		t.Error("expected an error for a stream with VolumeWritable=false")
	}
}

func TestPulseBackendGetVolumeUsesNormNotLinear(t *testing.T) {
	// Regression test for the "not Volume.Linear(), despite the name"
	// footgun: Linear() is the cubic amplitude conversion, and using it
	// here would show a different number than pavucontrol/KDE/pactl.
	fr := &fakeRequester{handle: func(req proto.RequestArgs, reply proto.Reply) error {
		*reply.(*proto.GetSinkInfoReply) = proto.GetSinkInfoReply{
			ChannelVolumes: proto.ChannelVolumes{proto.NormVolume(0.5)},
		}
		return nil
	}}
	b := &pulseBackend{client: fr, opts: Options{MaxPercent: DefaultMaxPercent}}
	state, err := b.GetVolume(t.Context(), Ref{Kind: RefSink, ID: "s"})
	if err != nil {
		t.Fatalf("GetVolume: %v", err)
	}
	if state.Percent != 50 {
		t.Errorf("Percent = %v, want 50 (Norm-based); Linear-based would incorrectly give 12.5", state.Percent)
	}
}

// --- ChannelVolumes helpers ---

func TestChannelVolumesToStateEmptyErrors(t *testing.T) {
	if _, err := channelVolumesToState(nil); err == nil {
		t.Error("expected an error for an empty ChannelVolumes (Avg() would otherwise panic dividing by zero)")
	}
}

func TestScaleChannelVolumesPreservesBalance(t *testing.T) {
	// 100%/50% (a 2:1 balance) scaled down to a 50% average must keep
	// that ratio, not flatten both channels to 50%.
	cv := proto.ChannelVolumes{proto.NormVolume(1.0), proto.NormVolume(0.5)}
	out := scaleChannelVolumes(cv, 50, DefaultMaxPercent)
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2", len(out))
	}
	got0, got1 := out[0].Norm()*100, out[1].Norm()*100
	const eps = 0.01
	if abs(got0-66.666667) > eps || abs(got1-33.333333) > eps {
		t.Errorf("out = [%.4f%%, %.4f%%], want ~[66.67%%, 33.33%%] (2:1 ratio preserved)", got0, got1)
	}
}

func TestScaleChannelVolumesClampsScaleFactorNotPerChannel(t *testing.T) {
	// Raising a 100%/50% stream toward a 150% average must clamp the
	// SCALE FACTOR so the loudest channel hits exactly the ceiling,
	// preserving the 2:1 ratio (150%/75%) — clamping each channel
	// independently would instead produce 150%/75%... no: it would clamp
	// channel 0 to 150 and channel 1 to whatever naive scaling gave it
	// (100%), flattening the ratio to 1.5:1.
	cv := proto.ChannelVolumes{proto.NormVolume(1.0), proto.NormVolume(0.5)}
	out := scaleChannelVolumes(cv, 150, 150)
	got0, got1 := out[0].Norm()*100, out[1].Norm()*100
	const eps = 0.01
	if abs(got0-150) > eps {
		t.Errorf("loudest channel = %.4f%%, want exactly 150%% (the ceiling)", got0)
	}
	if abs(got1-75) > eps {
		t.Errorf("quieter channel = %.4f%%, want 75%% (2:1 ratio preserved against the clamped scale)", got1)
	}
}

func TestScaleChannelVolumesZeroAverageSetsAllChannelsEqual(t *testing.T) {
	cv := proto.ChannelVolumes{proto.VolumeMuted, proto.VolumeMuted}
	out := scaleChannelVolumes(cv, 50, DefaultMaxPercent)
	for i, v := range out {
		if got := v.Norm() * 100; abs(got-50) > 0.01 {
			t.Errorf("out[%d] = %.4f%%, want 50%% (no ratio to preserve from silence)", i, got)
		}
	}
}

func TestScaleChannelVolumesEmptyFallsBackToStereo(t *testing.T) {
	out := scaleChannelVolumes(nil, 50, DefaultMaxPercent)
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2 (stereo fallback)", len(out))
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// --- dispatcher / Subscribe event translation ---

func TestDispatcherTranslatesSinkInputChangeEvent(t *testing.T) {
	fr := &fakeRequester{handle: func(req proto.RequestArgs, reply proto.Reply) error {
		switch r := req.(type) {
		case *proto.Subscribe:
			return nil
		case *proto.GetSinkInputInfo:
			if r.SinkInputIndex != 42 {
				t.Errorf("SinkInputIndex = %d, want 42", r.SinkInputIndex)
			}
			*reply.(*proto.GetSinkInputInfoReply) = proto.GetSinkInputInfoReply{
				SinkInputIndex: 42,
				MediaName:      "Playback",
				ChannelVolumes: proto.ChannelVolumes{proto.NormVolume(0.8)},
				Muted:          false,
				VolumeWritable: true,
				Properties:     proto.PropList{"application.name": proto.PropListString("vesktop")},
			}
			return nil
		default:
			t.Fatalf("unexpected request %T", req)
			return nil
		}
	}}

	d := newDispatcher(fr, testLogger())
	if err := d.start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer d.stop()

	ch := d.subscribe(t.Context())
	d.onSubscribeEvent(&proto.SubscribeEvent{Event: proto.EventSinkSinkInput | proto.EventChange, Index: 42})

	select {
	case ev := <-ch:
		if ev.Kind != EventStreamChanged {
			t.Errorf("Kind = %v, want EventStreamChanged", ev.Kind)
		}
		if ev.Stream == nil || ev.Stream.ID != "42" {
			t.Errorf("Stream = %+v, want ID 42", ev.Stream)
		}
		if ev.State == nil || abs(ev.State.Percent-80) > 0.01 {
			t.Errorf("State = %+v, want Percent ~80", ev.State)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for translated event")
	}
}

func TestDispatcherRemoveEventUsesLastKnownProps(t *testing.T) {
	fr := &fakeRequester{handle: func(req proto.RequestArgs, reply proto.Reply) error {
		switch req.(type) {
		case *proto.Subscribe:
			return nil
		case *proto.GetSinkInputInfo:
			*reply.(*proto.GetSinkInputInfoReply) = proto.GetSinkInputInfoReply{
				SinkInputIndex: 7,
				ChannelVolumes: proto.ChannelVolumes{proto.VolumeNorm},
				VolumeWritable: true,
				Properties:     proto.PropList{"application.name": proto.PropListString("vesktop")},
			}
			return nil
		default:
			t.Fatalf("unexpected request %T", req)
			return nil
		}
	}}
	d := newDispatcher(fr, testLogger())
	if err := d.start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer d.stop()

	ch := d.subscribe(t.Context())
	d.onSubscribeEvent(&proto.SubscribeEvent{Event: proto.EventSinkSinkInput | proto.EventNew, Index: 7})
	<-ch // drain the "new" translation

	d.onSubscribeEvent(&proto.SubscribeEvent{Event: proto.EventSinkSinkInput | proto.EventRemove, Index: 7})
	select {
	case ev := <-ch:
		if ev.Kind != EventStreamRemoved {
			t.Errorf("Kind = %v, want EventStreamRemoved", ev.Kind)
		}
		if ev.Stream == nil || ev.Stream.Props["application.name"] != "vesktop" {
			t.Errorf("Stream = %+v, want Props carried over from the last known state", ev.Stream)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for remove event")
	}
}

func TestDispatcherServerEventFiresDefaultChanged(t *testing.T) {
	fr := &fakeRequester{handle: func(req proto.RequestArgs, reply proto.Reply) error { return nil }}
	d := newDispatcher(fr, testLogger())
	if err := d.start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer d.stop()

	ch := d.subscribe(t.Context())
	d.onSubscribeEvent(&proto.SubscribeEvent{Event: proto.EventServer | proto.EventChange})

	select {
	case ev := <-ch:
		if ev.Kind != EventDefaultChanged {
			t.Errorf("Kind = %v, want EventDefaultChanged", ev.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for default-changed event")
	}
}

func TestDispatcherFanOutDropsRatherThanBlocks(t *testing.T) {
	fr := &fakeRequester{handle: func(req proto.RequestArgs, reply proto.Reply) error {
		if r, ok := req.(*proto.GetSinkInputInfo); ok {
			*reply.(*proto.GetSinkInputInfoReply) = proto.GetSinkInputInfoReply{
				SinkInputIndex: r.SinkInputIndex,
				ChannelVolumes: proto.ChannelVolumes{proto.VolumeNorm},
				VolumeWritable: true,
			}
		}
		return nil
	}}
	d := newDispatcher(fr, testLogger())
	if err := d.start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer d.stop()

	ch := d.subscribe(t.Context()) // never drained
	done := make(chan struct{})
	go func() {
		for i := uint32(0); i < subBufferSize+20; i++ {
			d.onSubscribeEvent(&proto.SubscribeEvent{Event: proto.EventSinkSinkInput | proto.EventChange, Index: i})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatcher deadlocked instead of dropping events for a full subscriber")
	}
	_ = ch
}

func TestDispatcherHeartbeatDetectsSilentConnectionLoss(t *testing.T) {
	dead := errors.New("connection reset by peer") // deliberately not a proto.Error
	fr := &fakeRequester{handle: func(req proto.RequestArgs, reply proto.Reply) error {
		if _, ok := req.(*proto.GetServerInfo); ok {
			return dead
		}
		return nil
	}}
	d := newDispatcher(fr, testLogger())
	d.heartbeat = 10 * time.Millisecond
	if err := d.start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	ch := d.subscribe(t.Context())
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected the subscriber channel to be closed after a heartbeat failure, got an event instead")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for heartbeat failure to close subscriber channels")
	}
}

// --- AnnotateProcessBinaries ---

func TestAnnotateProcessBinariesReadsProcExeSymlink(t *testing.T) {
	dir := t.TempDir()
	pidDir := dir + "/1234"
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink("/usr/bin/vesktop", pidDir+"/exe"); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	streams := []Stream{{
		ID:    "1",
		Props: map[string]string{"application.process.id": "1234"},
	}}
	AnnotateProcessBinaries(streams, dir)

	if got := streams[0].Props[propBinaryKnobd]; got != "vesktop" {
		t.Errorf("Props[%q] = %q, want %q", propBinaryKnobd, got, "vesktop")
	}
	if _, ok := streams[0].Props[propBinaryReal]; ok {
		t.Error("AnnotateProcessBinaries must never write application.process.binary itself — only the knobd.* namespaced key")
	}
}

func TestAnnotateProcessBinariesSkipsWhenBinaryAlreadyKnown(t *testing.T) {
	streams := []Stream{{
		ID: "1",
		Props: map[string]string{
			"application.process.id":     "1234",
			"application.process.binary": "brave",
		},
	}}
	AnnotateProcessBinaries(streams, t.TempDir())
	if _, ok := streams[0].Props[propBinaryKnobd]; ok {
		t.Error("must not annotate when application.process.binary is already present")
	}
}

func TestAnnotateProcessBinariesIgnoresUnreadableProc(t *testing.T) {
	// No such pid directory at all — must be treated as "no answer", not
	// an error, and must not poison Props.
	streams := []Stream{{
		ID:    "1",
		Props: map[string]string{"application.process.id": "99999"},
	}}
	AnnotateProcessBinaries(streams, t.TempDir())
	if _, ok := streams[0].Props[propBinaryKnobd]; ok {
		t.Error("must not set a knobd.process.binary when /proc/<pid>/exe can't be read")
	}
}
