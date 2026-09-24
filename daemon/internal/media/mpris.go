package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// ErrNoSessionBus is returned by New when no D-Bus session bus is
// reachable at all -- mirrors focus.ErrNoSessionBus. cmd/knobd treats
// this the same as any other media startup failure: log a warning and
// fall back to Unavailable(), never fatal.
var ErrNoSessionBus = errors.New("media: no D-Bus session bus available")

// mprisPrefix is every MPRIS player's bus name prefix.
const mprisPrefix = "org.mpris.MediaPlayer2."

const (
	rootIface     = "org.mpris.MediaPlayer2"
	playerIface   = "org.mpris.MediaPlayer2.Player"
	propsIface    = "org.freedesktop.DBus.Properties"
	playerObjPath = dbus.ObjectPath("/org/mpris/MediaPlayer2")
)

// propertyTimeout bounds every blocking Properties.GetAll/GetNameOwner
// call New's discovery loop makes on appear -- a hung player must not
// wedge that loop for longer than this.
const propertyTimeout = 2 * time.Second

// Options configures New.
type Options struct {
	Logger *slog.Logger

	// Conn, if non-nil, is used instead of dialing a new session bus
	// connection, and is never closed by Close -- for tests, mirroring
	// focus.Options.Conn.
	Conn *dbus.Conn
}

func (o *Options) setDefaults() {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
}

// mprisBackend is the real, session-bus-backed Backend.
type mprisBackend struct {
	log      *slog.Logger
	conn     *dbus.Conn
	ownsConn bool

	mu     sync.Mutex
	state  map[string]PlayerInfo // busName -> last known state
	owners map[string]string     // unique name (":1.50") -> busName

	sigCh chan *dbus.Signal
}

// New connects to the session bus. It fails only when no session bus is
// reachable at all (ErrNoSessionBus) -- matching focus.New's posture,
// see cmd/knobd/main.go's comment on why no backend at startup is
// fatal.
func New(ctx context.Context, opts Options) (Backend, error) {
	opts.setDefaults()

	conn := opts.Conn
	ownsConn := conn == nil
	if ownsConn {
		c, err := dbus.ConnectSessionBus()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrNoSessionBus, err)
		}
		conn = c
	}

	return &mprisBackend{
		log:      opts.Logger,
		conn:     conn,
		ownsConn: ownsConn,
		state:    make(map[string]PlayerInfo),
		owners:   make(map[string]string),
	}, nil
}

// Watch implements Backend. It is meant to be called exactly once (by
// Tracker), matching focus.Provider.Watch's contract.
func (b *mprisBackend) Watch(ctx context.Context) (<-chan Event, error) {
	if err := b.conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg0Namespace(mprisPrefix[:len(mprisPrefix)-1]),
	); err != nil {
		return nil, fmt.Errorf("media: subscribe NameOwnerChanged: %w", err)
	}
	if err := b.conn.AddMatchSignal(
		dbus.WithMatchInterface(propsIface),
		dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchObjectPath(playerObjPath),
	); err != nil {
		return nil, fmt.Errorf("media: subscribe PropertiesChanged: %w", err)
	}

	b.sigCh = make(chan *dbus.Signal, 32)
	b.conn.Signal(b.sigCh)

	events := make(chan Event, 16)
	go b.run(ctx, events)

	return events, nil
}

func (b *mprisBackend) run(ctx context.Context, events chan<- Event) {
	defer close(events)
	defer b.conn.RemoveSignal(b.sigCh)

	for _, name := range b.listPlayerNames() {
		if ev, ok := b.handleAppear(ctx, name); ok {
			b.send(ctx, events, ev)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case sig, ok := <-b.sigCh:
			if !ok {
				return
			}
			if ev, ok := b.handleSignal(ctx, sig); ok {
				b.send(ctx, events, ev)
			}
		}
	}
}

func (b *mprisBackend) send(ctx context.Context, events chan<- Event, ev Event) {
	select {
	case events <- ev:
	case <-ctx.Done():
	}
}

// listPlayerNames returns every currently-owned org.mpris.MediaPlayer2.*
// bus name, sorted, so callers (Tracker's initial ordering) get a
// deterministic starting point.
func (b *mprisBackend) listPlayerNames() []string {
	var names []string
	busObj := b.conn.BusObject()
	if err := busObj.Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		b.log.Warn("media: ListNames failed", "err", err)
		return nil
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if len(n) > len(mprisPrefix) && n[:len(mprisPrefix)] == mprisPrefix {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func (b *mprisBackend) handleSignal(ctx context.Context, sig *dbus.Signal) (Event, bool) {
	switch sig.Name {
	case "org.freedesktop.DBus.NameOwnerChanged":
		if len(sig.Body) != 3 {
			return Event{}, false
		}
		name, _ := sig.Body[0].(string)
		newOwner, _ := sig.Body[2].(string)
		if newOwner == "" {
			return b.handleVanish(name), true
		}
		return b.handleAppear(ctx, name)
	case "org.freedesktop.DBus.Properties.PropertiesChanged":
		return b.handlePropertiesChanged(ctx, sig)
	default:
		return Event{}, false
	}
}

func (b *mprisBackend) handleAppear(ctx context.Context, busName string) (Event, bool) {
	callCtx, cancel := context.WithTimeout(ctx, propertyTimeout)
	defer cancel()

	obj := b.conn.Object(busName, playerObjPath)

	var owner string
	if err := b.conn.BusObject().CallWithContext(callCtx, "org.freedesktop.DBus.GetNameOwner", 0, busName).Store(&owner); err != nil {
		b.log.Debug("media: GetNameOwner failed", "player", busName, "err", err)
		return Event{}, false
	}

	var rootProps, playerProps map[string]dbus.Variant
	if err := obj.CallWithContext(callCtx, propsIface+".GetAll", 0, rootIface).Store(&rootProps); err != nil {
		b.log.Debug("media: GetAll root properties failed", "player", busName, "err", err)
		rootProps = nil
	}
	if err := obj.CallWithContext(callCtx, propsIface+".GetAll", 0, playerIface).Store(&playerProps); err != nil {
		b.log.Debug("media: GetAll player properties failed", "player", busName, "err", err)
		playerProps = nil
	}

	info := PlayerInfo{BusName: busName}
	applyRootProperties(&info, rootProps)
	applyPlayerProperties(&info, playerProps, nil)

	b.mu.Lock()
	b.owners[owner] = busName
	b.state[busName] = info
	b.mu.Unlock()

	return Event{Kind: PlayerAppeared, Player: info}, true
}

func (b *mprisBackend) handleVanish(busName string) Event {
	b.mu.Lock()
	delete(b.state, busName)
	for unique, name := range b.owners {
		if name == busName {
			delete(b.owners, unique)
		}
	}
	b.mu.Unlock()

	return Event{Kind: PlayerVanished, Player: PlayerInfo{BusName: busName}}
}

func (b *mprisBackend) handlePropertiesChanged(ctx context.Context, sig *dbus.Signal) (Event, bool) {
	if len(sig.Body) != 3 {
		return Event{}, false
	}
	iface, _ := sig.Body[0].(string)
	changed, _ := sig.Body[1].(map[string]dbus.Variant)
	invalidated, _ := sig.Body[2].([]string)

	b.mu.Lock()
	busName, ok := b.owners[sig.Sender]
	if !ok {
		b.mu.Unlock()
		return Event{}, false
	}
	info := b.state[busName]
	b.mu.Unlock()

	switch iface {
	case rootIface:
		applyRootProperties(&info, changed)
	case playerIface:
		applyPlayerProperties(&info, changed, invalidated)
	default:
		return Event{}, false
	}

	b.mu.Lock()
	b.state[busName] = info
	b.mu.Unlock()

	_ = ctx
	return Event{Kind: PlayerChanged, Player: info}, true
}

// applyRootProperties merges org.mpris.MediaPlayer2's properties
// (currently just Identity) present in props into info.
func applyRootProperties(info *PlayerInfo, props map[string]dbus.Variant) {
	if v, ok := props["Identity"]; ok {
		if s, ok := v.Value().(string); ok {
			info.Identity = s
		}
	}
}

// applyPlayerProperties merges org.mpris.MediaPlayer2.Player's
// properties present in props into info, and clears any property named
// in invalidated back to its "unsupported" zero value (nil Shuffle,
// empty LoopStatus) -- see PlayerInfo's doc comment for why those two
// are pointer/empty-string rather than plain bool/string.
func applyPlayerProperties(info *PlayerInfo, props map[string]dbus.Variant, invalidated []string) {
	if v, ok := props["PlaybackStatus"]; ok {
		if s, ok := v.Value().(string); ok {
			info.Status = PlaybackStatus(s)
		}
	}
	if v, ok := props["CanControl"]; ok {
		if b, ok := v.Value().(bool); ok {
			info.CanControl = b
		}
	}
	if v, ok := props["CanSeek"]; ok {
		if b, ok := v.Value().(bool); ok {
			info.CanSeek = b
		}
	}
	if v, ok := props["Shuffle"]; ok {
		if b, ok := v.Value().(bool); ok {
			info.Shuffle = &b
		}
	}
	if v, ok := props["LoopStatus"]; ok {
		if s, ok := v.Value().(string); ok {
			info.LoopStatus = s
		}
	}
	if v, ok := props["Metadata"]; ok {
		if m, ok := v.Value().(map[string]dbus.Variant); ok {
			info.Track = decodeMetadata(m)
		}
	}
	for _, name := range invalidated {
		switch name {
		case "Shuffle":
			info.Shuffle = nil
		case "LoopStatus":
			info.LoopStatus = ""
		}
	}
}

// decodeMetadata decodes MPRIS's xesam:*/mpris:trackid Metadata
// dictionary into a Track. Any key it doesn't recognize, or whose value
// isn't the type the spec promises, is left at its zero value rather
// than erroring -- Metadata is exactly the kind of best-effort bag of
// hints audio.Stream/focus.AppInfo already are.
func decodeMetadata(m map[string]dbus.Variant) Track {
	var t Track
	if v, ok := m["mpris:trackid"]; ok {
		switch id := v.Value().(type) {
		case dbus.ObjectPath:
			t.ID = string(id)
		case string:
			t.ID = id
		}
	}
	if v, ok := m["xesam:title"]; ok {
		if s, ok := v.Value().(string); ok {
			t.Title = s
		}
	}
	if v, ok := m["xesam:artist"]; ok {
		if a, ok := v.Value().([]string); ok {
			t.Artists = a
		}
	}
	if v, ok := m["xesam:album"]; ok {
		if s, ok := v.Value().(string); ok {
			t.Album = s
		}
	}
	if v, ok := m["xesam:url"]; ok {
		if s, ok := v.Value().(string); ok {
			t.URL = s
		}
	}
	return t
}

func (b *mprisBackend) PlayPause(busName string) error {
	return b.conn.Object(busName, playerObjPath).Go(playerIface+".PlayPause", dbus.FlagNoReplyExpected, nil).Err
}

func (b *mprisBackend) Next(busName string) error {
	return b.conn.Object(busName, playerObjPath).Go(playerIface+".Next", dbus.FlagNoReplyExpected, nil).Err
}

func (b *mprisBackend) Previous(busName string) error {
	return b.conn.Object(busName, playerObjPath).Go(playerIface+".Previous", dbus.FlagNoReplyExpected, nil).Err
}

func (b *mprisBackend) Seek(busName string, offset time.Duration) error {
	micros := offset.Microseconds()
	return b.conn.Object(busName, playerObjPath).Go(playerIface+".Seek", dbus.FlagNoReplyExpected, nil, micros).Err
}

func (b *mprisBackend) SetShuffle(busName string, shuffle bool) error {
	return b.setProperty(busName, "Shuffle", shuffle)
}

func (b *mprisBackend) SetLoopStatus(busName string, status string) error {
	return b.setProperty(busName, "LoopStatus", status)
}

func (b *mprisBackend) setProperty(busName, prop string, value any) error {
	return b.conn.Object(busName, playerObjPath).Go(
		propsIface+".Set", dbus.FlagNoReplyExpected, nil,
		playerIface, prop, dbus.MakeVariant(value),
	).Err
}

func (b *mprisBackend) Close() error {
	if b.sigCh != nil {
		b.conn.RemoveSignal(b.sigCh)
	}
	if b.ownsConn {
		return b.conn.Close()
	}
	return nil
}

var _ Backend = (*mprisBackend)(nil)
