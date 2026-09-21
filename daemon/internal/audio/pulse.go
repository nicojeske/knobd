package audio

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/jfreymuth/pulse/proto"
)

// requestTimeout overrides proto.Client's 1-second default (set in
// proto.Connect), which is short enough that a slow enumeration during a
// device switch can fail an otherwise healthy connection.
const requestTimeout = 5 * time.Second

// subscribeMask is the explicit union of facilities Subscribe needs,
// rather than proto.SubscriptionMaskAll (which also brings in module/
// client/sample-cache noise). SourceInput is not a typo for
// "source-output" — it's the (slightly misleadingly named) mask bit this
// library uses for source-output/record-stream events; Server is what
// reports a default sink/source change, which Device.IsDefault and
// model.TargetDefaultSink/TargetDefaultSource depend on and which it
// would be easy to forget.
const subscribeMask = proto.SubscriptionMaskSinkInput | proto.SubscriptionMaskSourceInput |
	proto.SubscriptionMaskSink | proto.SubscriptionMaskSource | proto.SubscriptionMaskServer

// requester is the one method of *proto.Client that pulseBackend's
// mapping logic depends on. Depending on this instead of *proto.Client
// directly is what makes SetVolume's channel scaling, the
// proto.Undefined rule, the empty-ChannelVolumes guard, and event
// translation unit-testable under `go test ./internal/audio/...` with no
// PipeWire: a test fake can type-switch on the concrete *proto.Get*/
// *proto.Set* request types (proto.RequestArgs.command() is unexported,
// so a fake can't implement those request types itself, but it can
// still recognize and answer them) and fill in the reply.
type requester interface {
	Request(req proto.RequestArgs, reply proto.Reply) error
}

// pulseBackend implements Backend against the PulseAudio native
// protocol, via pipewire-pulse. See the package doc comment for why:
// confirmed against this system's PipeWire 1.6.8, protocol version 32
// negotiated (the client library's own cap; the server offers 35), no
// pactl fallback needed.
//
// Two library behaviors shape most of the design below, and are worth
// stating once here rather than at every call site:
//
//  1. proto.Client has NO Close method. The only way to stop its
//     internal readLoop goroutine is to close the net.Conn proto.Connect
//     handed back alongside the client — pulseBackend holds onto it for
//     exactly that reason (closeConn, below).
//  2. On a timeout, proto.Client.Request returns but does NOT delete its
//     pending awaitReply entry — if the reply arrives after the caller
//     has already moved on, the library writes into the reply struct the
//     caller already returned from. Rule followed everywhere in this
//     file: allocate a fresh reply struct per call, and never read one
//     whose Request call returned a non-nil error.
type pulseBackend struct {
	client    requester
	closeConn func() error
	opts      Options

	dispatch *dispatcher
}

func newPulseBackend(ctx context.Context, opts Options) (Backend, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.ProcRoot == "" {
		opts.ProcRoot = "/proc"
	}
	if opts.MaxPercent <= 0 {
		opts.MaxPercent = DefaultMaxPercent
	}

	type connectResult struct {
		client *proto.Client
		conn   net.Conn
		err    error
	}
	resultCh := make(chan connectResult, 1)
	go func() {
		client, conn, err := proto.Connect("")
		resultCh <- connectResult{client, conn, err}
	}()

	var client *proto.Client
	var conn net.Conn
	select {
	case r := <-resultCh:
		if r.err != nil {
			return nil, fmt.Errorf("audio: connect to pipewire-pulse: %w", r.err)
		}
		client, conn = r.client, r.conn
	case <-ctx.Done():
		// Connect may still succeed after we give up on it; when it
		// does, close what it opened rather than leaking the connection
		// and its read-loop goroutine.
		go func() {
			if r := <-resultCh; r.conn != nil {
				r.conn.Close()
			}
		}()
		return nil, ctx.Err()
	}

	client.SetTimeout(requestTimeout)

	dispatch := newDispatcher(client, opts.logger())
	client.Callback = func(msg interface{}) {
		switch m := msg.(type) {
		case *proto.SubscribeEvent:
			dispatch.onSubscribeEvent(m)
		case *proto.ConnectionClosed:
			dispatch.onConnectionClosed()
		}
	}

	var name proto.SetClientNameReply
	if err := client.Request(&proto.SetClientName{Props: proto.PropList{
		"application.name": proto.PropListString("knobd"),
	}}, &name); err != nil {
		conn.Close()
		return nil, fmt.Errorf("audio: SetClientName: %w", err)
	}

	if err := dispatch.start(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("audio: subscribe: %w", err)
	}

	return &pulseBackend{
		client:    client,
		closeConn: conn.Close,
		opts:      opts,
		dispatch:  dispatch,
	}, nil
}

func (b *pulseBackend) Close() error {
	b.dispatch.stop()
	return b.closeConn()
}

// --- enumeration ---

func (b *pulseBackend) Sinks(ctx context.Context) ([]Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var list proto.GetSinkInfoListReply
	if err := b.client.Request(&proto.GetSinkInfoList{}, &list); err != nil {
		return nil, fmt.Errorf("audio: GetSinkInfoList: %w", err)
	}
	def, err := b.defaultNames()
	if err != nil {
		return nil, err
	}
	devices := make([]Device, 0, len(list))
	for _, s := range list {
		devices = append(devices, Device{
			ID:          s.SinkName,
			Description: sinkDescription(s),
			IsDefault:   s.SinkName == def.sink,
		})
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].ID < devices[j].ID })
	return devices, nil
}

func sinkDescription(s *proto.GetSinkInfoReply) string {
	// s.Device is, despite the name, the protocol's sink *description*
	// field (PulseAudio's wire format calls it that; the Go struct field
	// is just named after the C field it decodes). Properties'
	// node.description is a PipeWire-native fallback for anything that
	// doesn't populate it.
	if s.Device != "" {
		return s.Device
	}
	if s.Properties != nil {
		if v, ok := s.Properties["node.description"]; ok {
			return v.String()
		}
	}
	return s.SinkName
}

func (b *pulseBackend) Sources(ctx context.Context) ([]Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var list proto.GetSourceInfoListReply
	if err := b.client.Request(&proto.GetSourceInfoList{}, &list); err != nil {
		return nil, fmt.Errorf("audio: GetSourceInfoList: %w", err)
	}
	def, err := b.defaultNames()
	if err != nil {
		return nil, err
	}
	devices := make([]Device, 0, len(list))
	for _, s := range list {
		// Every sink has a matching ".monitor" source PipeWire creates
		// automatically; those are implementation plumbing, not
		// something a user would ever want to bind a knob to, so they're
		// filtered out here rather than left for the UI to hide.
		if s.Properties != nil {
			if v, ok := s.Properties["device.class"]; ok && v.String() == "monitor" {
				continue
			}
		}
		devices = append(devices, Device{
			ID:          s.SourceName,
			Description: sourceDescription(s),
			IsDefault:   s.SourceName == def.source,
		})
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].ID < devices[j].ID })
	return devices, nil
}

func sourceDescription(s *proto.GetSourceInfoReply) string {
	if s.Device != "" {
		return s.Device
	}
	if s.Properties != nil {
		if v, ok := s.Properties["node.description"]; ok {
			return v.String()
		}
	}
	return s.SourceName
}

type defaultDeviceNames struct{ sink, source string }

func (b *pulseBackend) defaultNames() (defaultDeviceNames, error) {
	var srv proto.GetServerInfoReply
	if err := b.client.Request(&proto.GetServerInfo{}, &srv); err != nil {
		return defaultDeviceNames{}, fmt.Errorf("audio: GetServerInfo: %w", err)
	}
	return defaultDeviceNames{sink: srv.DefaultSinkName, source: srv.DefaultSourceName}, nil
}

func (b *pulseBackend) Streams(ctx context.Context) ([]Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var sinkInputs proto.GetSinkInputInfoListReply
	if err := b.client.Request(&proto.GetSinkInputInfoList{}, &sinkInputs); err != nil {
		return nil, fmt.Errorf("audio: GetSinkInputInfoList: %w", err)
	}
	var sourceOutputs proto.GetSourceOutputInfoListReply
	if err := b.client.Request(&proto.GetSourceOutputInfoList{}, &sourceOutputs); err != nil {
		return nil, fmt.Errorf("audio: GetSourceOutputInfoList: %w", err)
	}

	streams := make([]Stream, 0, len(sinkInputs)+len(sourceOutputs))
	for _, si := range sinkInputs {
		streams = append(streams, sinkInputToStream(si))
	}
	for _, so := range sourceOutputs {
		streams = append(streams, sourceOutputToStream(so))
	}
	AnnotateProcessBinaries(streams, b.opts.ProcRoot)
	sort.Slice(streams, func(i, j int) bool { return streams[i].ID < streams[j].ID })
	return streams, nil
}

func sinkInputToStream(si *proto.GetSinkInputInfoReply) Stream {
	props := propListToMap(si.Properties)
	props[propMediaName] = si.MediaName
	props["knobd.stream.corked"] = strconv.FormatBool(si.Corked)
	return Stream{
		ID:        strconv.FormatUint(uint64(si.SinkInputIndex), 10),
		Direction: StreamPlayback,
		Props:     props,
	}
}

func sourceOutputToStream(so *proto.GetSourceOutputInfoReply) Stream {
	props := propListToMap(so.Properties)
	props[propMediaName] = so.MediaName
	props["knobd.stream.corked"] = strconv.FormatBool(so.Corked)
	return Stream{
		// Note: the library's own field is spelled SourceOutpuIndex
		// (missing the 't') — a typo in github.com/jfreymuth/pulse
		// v0.1.3 itself, not a mistake here.
		ID:        strconv.FormatUint(uint64(so.SourceOutpuIndex), 10),
		Direction: StreamRecord,
		Props:     props,
	}
}

func propListToMap(pl proto.PropList) map[string]string {
	m := make(map[string]string, len(pl))
	for k, v := range pl {
		m[k] = v.String()
	}
	return m
}

// --- volume / mute ---

func (b *pulseBackend) GetVolume(ctx context.Context, ref Ref) (VolumeState, error) {
	if err := ctx.Err(); err != nil {
		return VolumeState{}, err
	}
	cv, muted, err := b.currentVolume(ref)
	if err != nil {
		return VolumeState{}, err
	}
	state, err := channelVolumesToState(cv)
	if err != nil {
		return VolumeState{}, err
	}
	state.Muted = muted
	return state, nil
}

// currentVolume fetches ref's current ChannelVolumes and mute state.
// SetVolume needs this first for two reasons, not just balance
// preservation: the server rejects a ChannelVolumes whose length doesn't
// match the entity's actual channel count (ErrInvalidArgument), and
// scaling every channel by the same factor is what preserves whatever
// balance the user set in pavucontrol rather than flattening it.
func (b *pulseBackend) currentVolume(ref Ref) (proto.ChannelVolumes, bool, error) {
	switch ref.Kind {
	case RefSink:
		var info proto.GetSinkInfoReply
		if err := b.client.Request(&proto.GetSinkInfo{SinkIndex: proto.Undefined, SinkName: ref.ID}, &info); err != nil {
			return nil, false, fmt.Errorf("audio: GetSinkInfo(%s): %w", ref.ID, err)
		}
		return info.ChannelVolumes, info.Mute, nil
	case RefSource:
		var info proto.GetSourceInfoReply
		if err := b.client.Request(&proto.GetSourceInfo{SourceIndex: proto.Undefined, SourceName: ref.ID}, &info); err != nil {
			return nil, false, fmt.Errorf("audio: GetSourceInfo(%s): %w", ref.ID, err)
		}
		return info.ChannelVolumes, info.Mute, nil
	case RefStream:
		idx, err := parseIndex(ref.ID)
		if err != nil {
			return nil, false, err
		}
		var info proto.GetSinkInputInfoReply
		if err := b.client.Request(&proto.GetSinkInputInfo{SinkInputIndex: idx}, &info); err != nil {
			return nil, false, fmt.Errorf("audio: GetSinkInputInfo(%s): %w", ref.ID, err)
		}
		if !info.VolumeWritable {
			return nil, false, fmt.Errorf("audio: stream %s does not support volume control", ref.ID)
		}
		return info.ChannelVolumes, info.Muted, nil
	case RefRecord:
		idx, err := parseIndex(ref.ID)
		if err != nil {
			return nil, false, err
		}
		var info proto.GetSourceOutputInfoReply
		if err := b.client.Request(&proto.GetSourceOutputInfo{SourceOutpuIndex: idx}, &info); err != nil {
			return nil, false, fmt.Errorf("audio: GetSourceOutputInfo(%s): %w", ref.ID, err)
		}
		if !info.VolumeWritable {
			return nil, false, fmt.Errorf("audio: record stream %s does not support volume control", ref.ID)
		}
		return info.ChannelVolumes, info.Muted, nil
	default:
		return nil, false, fmt.Errorf("audio: unknown ref kind %q", ref.Kind)
	}
}

func parseIndex(id string) (uint32, error) {
	n, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("audio: invalid stream id %q: %w", id, err)
	}
	return uint32(n), nil
}

// channelVolumesToState converts a proto.ChannelVolumes into the percent
// scale pactl/pavucontrol/KDE display (Avg().Norm()*100 — the ratio to
// proto.VolumeNorm), NOT proto.Volume.Linear(), despite Linear's name
// reading like the one you'd want: Linear() is the cubic
// amplitude-from-percent conversion PipeWire applies internally, and
// using it here would silently show the wrong number next to every other
// volume UI on the system.
func channelVolumesToState(cv proto.ChannelVolumes) (VolumeState, error) {
	if len(cv) == 0 {
		// ChannelVolumes.Avg() divides by len(cv) with no guard and
		// panics on empty input — a stream that hasn't finished
		// negotiating a format yet can legitimately have none.
		return VolumeState{}, errors.New("audio: entity has no channels (not yet negotiated?)")
	}
	channels := make([]float64, len(cv))
	for i, v := range cv {
		channels[i] = v.Norm() * 100
	}
	return VolumeState{Percent: cv.Avg().Norm() * 100, Channels: channels}, nil
}

func (b *pulseBackend) SetVolume(ctx context.Context, ref Ref, percent float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if percent < 0 {
		percent = 0
	}
	if max := b.opts.MaxPercent; max > 0 && percent > max {
		percent = max
	}

	cv, _, err := b.currentVolume(ref)
	if err != nil {
		return err
	}
	newCV := scaleChannelVolumes(cv, percent, b.opts.MaxPercent)

	switch ref.Kind {
	case RefSink:
		return b.client.Request(&proto.SetSinkVolume{SinkIndex: proto.Undefined, SinkName: ref.ID, ChannelVolumes: newCV}, nil)
	case RefSource:
		return b.client.Request(&proto.SetSourceVolume{SourceIndex: proto.Undefined, SourceName: ref.ID, ChannelVolumes: newCV}, nil)
	case RefStream:
		idx, err := parseIndex(ref.ID)
		if err != nil {
			return err
		}
		return b.client.Request(&proto.SetSinkInputVolume{SinkInputIndex: idx, ChannelVolumes: newCV}, nil)
	case RefRecord:
		idx, err := parseIndex(ref.ID)
		if err != nil {
			return err
		}
		return b.client.Request(&proto.SetSourceOutputVolume{SourceOutputIndex: idx, ChannelVolumes: newCV}, nil)
	default:
		return fmt.Errorf("audio: unknown ref kind %q", ref.Kind)
	}
}

// scaleChannelVolumes returns cv scaled so its average becomes
// targetPercent, preserving each channel's relative balance. The scale
// factor — not each resulting channel individually — is clamped to
// maxPercent: clamping per-channel after scaling would flatten whatever
// balance the user set in pavucontrol (e.g. [100%, 50%] scaled toward
// 150% average would clamp only the loud channel, turning a 2:1 balance
// into something close to 1:1). When the current average is zero (fully
// muted-by-volume) there's no ratio to preserve, so every channel is set
// equal instead.
func scaleChannelVolumes(cv proto.ChannelVolumes, targetPercent, maxPercent float64) proto.ChannelVolumes {
	if maxPercent <= 0 {
		maxPercent = DefaultMaxPercent
	}
	if len(cv) == 0 {
		// No channel count to preserve; the caller already rejected this
		// case for GetVolume, but SetVolume can still reach it if the
		// entity hasn't negotiated a format. Fall back to stereo, the
		// overwhelmingly common case, rather than sending an empty
		// ChannelVolumes the server will reject.
		cv = make(proto.ChannelVolumes, 2)
	}

	avg := cv.Avg().Norm()
	target := targetPercent / 100

	out := make(proto.ChannelVolumes, len(cv))
	if avg <= 0 {
		for i := range out {
			out[i] = proto.NormVolume(target)
		}
		return out
	}

	scale := target / avg
	// Clamp the scale factor so the loudest channel never exceeds
	// maxPercent, rather than clamping each channel after the fact.
	maxNorm := maxPercent / 100
	loudest := 0.0
	for _, v := range cv {
		if n := v.Norm(); n > loudest {
			loudest = n
		}
	}
	if loudest > 0 {
		if capScale := maxNorm / loudest; scale > capScale {
			scale = capScale
		}
	}
	for i, v := range cv {
		out[i] = proto.NormVolume(v.Norm() * scale)
	}
	return out
}

func (b *pulseBackend) SetMute(ctx context.Context, ref Ref, muted bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch ref.Kind {
	case RefSink:
		return b.client.Request(&proto.SetSinkMute{SinkIndex: proto.Undefined, SinkName: ref.ID, Mute: muted}, nil)
	case RefSource:
		return b.client.Request(&proto.SetSourceMute{SourceIndex: proto.Undefined, SourceName: ref.ID, Mute: muted}, nil)
	case RefStream:
		idx, err := parseIndex(ref.ID)
		if err != nil {
			return err
		}
		return b.client.Request(&proto.SetSinkInputMute{SinkInputIndex: idx, Mute: muted}, nil)
	case RefRecord:
		idx, err := parseIndex(ref.ID)
		if err != nil {
			return err
		}
		return b.client.Request(&proto.SetSourceOutputMute{SourceOutputIndex: idx, Mute: muted}, nil)
	default:
		return fmt.Errorf("audio: unknown ref kind %q", ref.Kind)
	}
}

func (b *pulseBackend) Subscribe(ctx context.Context) (<-chan Event, error) {
	return b.dispatch.subscribe(ctx), nil
}

// --- /proc/<pid>/exe fallback ---

// AnnotateProcessBinaries fills in a synthesized "knobd.process.binary"
// property on any stream that publishes application.process.id but not
// application.process.binary, by reading procRoot+"/<pid>/exe"'s
// basename. It writes to a knobd-namespaced key rather than overwriting
// application.process.binary so a fabricated value is never confused
// with one PipeWire actually published — M07's config UI can tell the
// two apart, and Resolve (matcher.go) checks the real key first, this
// one second.
//
// This is applied at enumeration time (Streams calls it), not inside
// Resolve, specifically so Resolve stays a pure function testable
// against testdata/pipewire/pw-dump-sample.json with no filesystem
// access at all.
//
// Any error is treated as "no answer" and never poisons Props: a
// process's /proc/<pid>/exe can be unreadable (EACCES, for a process we
// don't own) or can resolve to something misleading — for a Flatpak/
// bubblewrap-sandboxed app, application.process.id is the pid inside the
// app's own pid namespace, which is a different process (or nothing) in
// ours. There is no way to detect that case from here; it's a known
// sharp edge, documented in specs/milestones/M03-audio-control.md.
func AnnotateProcessBinaries(streams []Stream, procRoot string) {
	if procRoot == "" {
		procRoot = "/proc"
	}
	for i := range streams {
		p := streams[i].Props
		if p == nil {
			continue
		}
		if p[propBinaryReal] != "" {
			continue
		}
		pidStr := p["application.process.id"]
		if pidStr == "" {
			continue
		}
		exe, err := os.Readlink(filepath.Join(procRoot, pidStr, "exe"))
		if err != nil {
			continue
		}
		if base := filepath.Base(exe); base != "" && base != "." && base != "/" {
			p[propBinaryKnobd] = base
		}
	}
}

var _ Backend = (*pulseBackend)(nil)
