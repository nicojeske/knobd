package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/njeske/knobd/internal/api"
	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/engine"
	"github.com/njeske/knobd/internal/midi"
)

// connStatus is the mutex-guarded MIDI/audio connection status
// watchConnections writes and daemonState.State reads.
//
// midi.Supervisor.Connected/Disconnected and audio.Supervisor's
// equivalents are buffered-1, coalescing, single-consumer channels: a
// second reader would starve the first of events. cmd/knobd is that one
// consumer. M05's decision, recorded here as M04 asked: keep ownership
// in watchConnections rather than adding fan-out to the supervisors --
// it already was the sole consumer, and it's the natural place to also
// trigger engine.Engine.RepaintLEDs on a device reconnect, needing no
// new channel of its own.
type connStatus struct {
	mu sync.Mutex

	deviceConnected bool
	deviceName      string
	devicePath      string
	deviceLastErr   string

	audioConnected bool
	audioLastErr   string
}

func (c *connStatus) setDeviceConnected(info midi.DeviceInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deviceConnected = true
	c.deviceName = info.Name
	c.devicePath = info.Path
}

func (c *connStatus) setDeviceDisconnected(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deviceConnected = false
	if err != nil {
		c.deviceLastErr = err.Error()
	}
}

func (c *connStatus) setAudioConnected() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.audioConnected = true
}

func (c *connStatus) setAudioDisconnected(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.audioConnected = false
	if err != nil {
		c.audioLastErr = err.Error()
	}
}

func (c *connStatus) snapshot() (api.DeviceState, api.AudioState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return api.DeviceState{
		Connected: c.deviceConnected,
		Name:      c.deviceName,
		Path:      c.devicePath,
		LastError: c.deviceLastErr,
	}, api.AudioState{
		Connected: c.audioConnected,
		LastError: c.audioLastErr,
	}
}

// watchConnections is connStatus's single consumer; see its doc comment
// for why there can only be one. Runs until ctx is done.
//
// On a device reconnect it also calls eng.RepaintLEDs: the X-Touch
// Mini's rings and buttons don't remember anything across a power
// cycle, so whatever knobd last believed it had pushed needs rewriting
// from scratch. RepaintLEDs is a channel round trip like SetConfig, so
// this blocks watchConnections' loop for its duration (a handful of
// MIDI writes) -- acceptable since Connected only fires on an actual
// reconnect, not a hot path.
func watchConnections(ctx context.Context, midiSup *midi.Supervisor, audioSup *audio.Supervisor, status *connStatus, eng *engine.Engine, log *slog.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		case info := <-midiSup.Connected():
			status.setDeviceConnected(info)
			if err := eng.RepaintLEDs(ctx); err != nil && ctx.Err() == nil {
				// Non-fatal: engine.Run may not be consuming yet during
				// startup's race between the two, or may have already
				// exited; either way there's nothing watchConnections
				// can do but log and keep tracking connection status.
				log.Warn("LED repaint after reconnect failed", "err", err)
			}
		case err := <-midiSup.Disconnected():
			status.setDeviceDisconnected(err)
		case <-audioSup.Connected():
			status.setAudioConnected()
		case err := <-audioSup.Disconnected():
			status.setAudioDisconnected(err)
		}
	}
}

// daemonState is cmd/knobd's api.StateProvider adapter.
type daemonState struct {
	eng            *engine.Engine
	status         *connStatus
	focusAvailable bool
}

// State implements api.StateProvider.
func (d *daemonState) State(ctx context.Context) (api.State, error) {
	snap, err := d.eng.Snapshot(ctx)
	if err != nil {
		return api.State{}, err
	}
	device, audioState := d.status.snapshot()
	return snapshotToState(snap, device, audioState, d.focusAvailable, time.Now()), nil
}

// snapshotToState is a pure translation from engine.Snapshot (plus the
// connection status cmd/knobd tracks separately, since engine has no
// reason to know about MIDI/audio connection lifecycle) to api.State.
// Kept free of goroutines and I/O so it's table-testable on its own.
func snapshotToState(snap engine.Snapshot, device api.DeviceState, audioState api.AudioState, focusAvailable bool, now time.Time) api.State {
	controls := make([]api.ControlState, 0, len(snap.Controls))
	for _, cs := range snap.Controls {
		out := api.ControlState{
			Control:    cs.Control,
			Gesture:    cs.Gesture,
			ActionType: cs.ActionType,
			Target:     cs.Target,
		}
		if len(cs.Refs) > 0 {
			refs := make([]string, len(cs.Refs))
			for i, ref := range cs.Refs {
				refs[i] = string(ref.Kind) + ":" + ref.ID
			}
			resolved := &api.ResolvedTarget{Refs: refs}
			if cs.Volume != nil {
				resolved.VolumePercent = cs.Volume.Percent
				resolved.Muted = cs.Volume.Muted
			}
			out.Resolved = resolved
		}
		controls = append(controls, out)
	}

	return api.State{
		Now:      now,
		Device:   device,
		Audio:    audioState,
		Focus:    api.FocusState{Available: focusAvailable, ResourceClass: snap.Focused.ResourceClass},
		Profile:  api.ProfileState{ActiveProfileID: snap.ActiveProfileID, ActiveLayer: snap.ActiveLayer},
		Controls: controls,
	}
}
