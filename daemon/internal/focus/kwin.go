package focus

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/njeske/knobd/internal/proctree"
)

// Default D-Bus coordinates knobd registers itself under, for
// script/knobd-focus.js's callDBus to reach. Options can override every
// one of these (kwin_test.go's manual-only integration test does, so it
// never collides with a real running daemon on the same session bus).
const (
	DefaultBusName    = "io.github.njeske.knobd"
	DefaultObjectPath = "/io/github/njeske/knobd/Focus"
	DefaultInterface  = "io.github.njeske.knobd.Focus1"
)

// kwinPluginName is the name knobd's script is registered under with
// KWin's Scripting interface -- the argument to loadScript/unloadScript/
// isScriptLoaded.
const kwinPluginName = "knobd"

// handshakeTimeout bounds how long New waits, in the background, before
// warning that no focus event has arrived at all yet. This -- not
// loadScript's return value (see scripting.go's loadScript doc comment)
// -- is the liveness signal this package actually trusts: callDBus
// failures are silent everywhere (no error in the daemon's log, no
// error in KWin's journal), so an absent handshake is the only signal
// available that something in the chain isn't working.
const handshakeTimeout = 3 * time.Second

// kwinRestartReinstallDelay is how long supervise waits after seeing
// org.kde.KWin reappear on the bus before reinstalling the script --
// /Scripting becomes callable slightly after the well-known name itself
// does.
const kwinRestartReinstallDelay = 500 * time.Millisecond

// Options configures New.
type Options struct {
	Logger *slog.Logger

	// ScriptPath, if set, is loaded verbatim instead of materializing
	// the embedded script/knobd-focus.js -- for iterating on the
	// script by hand without rebuilding the daemon.
	ScriptPath string

	BusName    string
	ObjectPath string
	Interface  string

	// ProcRoot overrides where AppInfo.Binary is resolved from ("" =
	// the real /proc). Tests set a t.TempDir() fabricated tree.
	ProcRoot string

	// Conn, if non-nil, is used instead of dialing a new session bus
	// connection, and is never closed by Close -- for tests that hold
	// their own connection lifecycle.
	Conn *dbus.Conn
}

func (o *Options) setDefaults() {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.BusName == "" {
		o.BusName = DefaultBusName
	}
	if o.ObjectPath == "" {
		o.ObjectPath = DefaultObjectPath
	}
	if o.Interface == "" {
		o.Interface = DefaultInterface
	}
}

// kwinProvider is the real, KWin-backed Provider -- see the package doc
// comment and specs/adr/0003-focus-tracking-via-kwin-script.md.
//
// It is deliberately structured so FocusChanged (the exported D-Bus
// method the script calls) is a plain Go method touching no bus state
// itself: decoding, filtering, /proc annotation, and publishing to
// Current/Watch all happen there with no D-Bus round trip, which is
// what lets kwin_test.go exercise the entire cache/fan-out path by
// constructing a kwinProvider and calling FocusChanged directly, with
// no live session bus at all.
type kwinProvider struct {
	log  *slog.Logger
	proc proctree.Walker
	conn *dbus.Conn
	// ownsConn is false when opts.Conn was supplied by the caller --
	// Close must never close a connection it didn't dial itself.
	ownsConn bool

	mu      sync.RWMutex
	current AppInfo
	known   bool // has any accepted event ever arrived, for warnIfNoHandshake

	subsMu sync.Mutex
	subs   []chan AppInfo

	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

// New connects to the session bus, registers knobd's own Focus1
// service, and installs/starts the KWin script (embedded, unless
// Options.ScriptPath overrides it) that reports focus changes back into
// it.
//
// New fails only for the two conditions retrying can never fix: no
// session bus reachable (ErrNoSessionBus) and its bus name already
// taken (ErrNameTaken). Matching the audio/MIDI supervisors' own
// startup posture (see cmd/knobd/main.go's comment on why no backend
// being present at startup is a fatal error): KWin not yet being on the
// bus, a script install failure, or no handshake ever arriving are all
// logged and left for the background supervise loop to retry -- never a
// fatal New error, since "focus tracking isn't up yet" must never turn
// into a knobd startup failure.
func New(ctx context.Context, opts Options) (Provider, error) {
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

	reply, err := conn.RequestName(opts.BusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		if ownsConn {
			conn.Close()
		}
		return nil, fmt.Errorf("focus: request bus name %q: %w", opts.BusName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		if ownsConn {
			conn.Close()
		}
		return nil, fmt.Errorf("%w: %q", ErrNameTaken, opts.BusName)
	}

	p := &kwinProvider{
		log:      opts.Logger,
		proc:     proctree.Walker{Root: opts.ProcRoot},
		conn:     conn,
		ownsConn: ownsConn,
		done:     make(chan struct{}),
	}

	if err := conn.Export(p, dbus.ObjectPath(opts.ObjectPath), opts.Interface); err != nil {
		if ownsConn {
			conn.Close()
		}
		return nil, fmt.Errorf("focus: export %s at %s: %w", opts.Interface, opts.ObjectPath, err)
	}

	scriptPath := opts.ScriptPath
	if scriptPath == "" {
		path, err := materializeScript(scriptDir(), opts.BusName, opts.ObjectPath, opts.Interface)
		if err != nil {
			if ownsConn {
				conn.Close()
			}
			return nil, fmt.Errorf("focus: materialize KWin script: %w", err)
		}
		scriptPath = path
	}

	runCtx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	go p.supervise(runCtx, scriptPath)

	return p, nil
}

// FocusChanged is exported over D-Bus as opts.Interface's FocusChanged
// method, with no return value on success -- script/knobd-focus.js's
// callDBus calls this directly. See the type doc comment for why
// everything it does is plain Go with no bus access.
func (p *kwinProvider) FocusChanged(payload string) *dbus.Error {
	ev, err := decodeScriptEvent(payload)
	if err != nil {
		p.log.Warn("focus: malformed payload from the KWin script", "err", err)
		return dbus.NewError("io.github.njeske.knobd.Focus1.Error.BadPayload", []any{err.Error()})
	}

	p.mu.Lock()
	p.known = true
	p.mu.Unlock()

	if !acceptEvent(ev) {
		return nil
	}

	info := ev.appInfo()
	if info.PID > 0 {
		// One readlink, done here (off the engine's goroutine
		// entirely) rather than in resolveFocused, which must stay a
		// pure cache read -- see AppInfo.Binary's doc comment and
		// engine/resolver.go.
		info.Binary = p.proc.ExeName(info.PID)
	}

	p.publish(info)
	return nil
}

func (p *kwinProvider) publish(info AppInfo) {
	p.mu.Lock()
	p.current = info
	p.mu.Unlock()

	p.subsMu.Lock()
	subs := append([]chan AppInfo(nil), p.subs...)
	p.subsMu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- info:
		default:
			// Drop-oldest: make room for the latest value rather than
			// ever block this callback on a stalled subscriber. These
			// are state snapshots, not increments -- the newest value
			// is always the correct one to keep, whichever of the
			// (extremely unlikely, since Export runs each call in its
			// own goroutine) concurrent callbacks wins this race.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- info:
			default:
			}
		}
	}
}

// Watch implements Provider.
func (p *kwinProvider) Watch(ctx context.Context) (<-chan AppInfo, error) {
	ch := make(chan AppInfo, 8)
	p.subsMu.Lock()
	p.subs = append(p.subs, ch)
	p.subsMu.Unlock()

	go func() {
		<-ctx.Done()
		p.subsMu.Lock()
		defer p.subsMu.Unlock()
		for i, c := range p.subs {
			if c == ch {
				p.subs = append(p.subs[:i], p.subs[i+1:]...)
				break
			}
		}
		close(ch)
	}()

	return ch, nil
}

// Current implements Provider. Never blocks on I/O -- see Provider's
// doc comment.
func (p *kwinProvider) Current(context.Context) (AppInfo, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.current, nil
}

// Close implements Provider. Idempotent. Safe to call on a kwinProvider
// built directly by a test (with no conn and no supervise goroutine
// running) rather than through New.
func (p *kwinProvider) Close() error {
	p.closeOnce.Do(func() {
		if p.cancel != nil {
			p.cancel()
			<-p.done
		}
		if p.conn == nil {
			return
		}
		if err := newKWinScripting(p.conn).unloadScript(kwinPluginName); err != nil {
			p.log.Debug("focus: unload KWin script on close failed", "err", err)
		}
		if p.ownsConn {
			p.conn.Close()
		}
	})
	return nil
}

var _ Provider = (*kwinProvider)(nil)

// supervise installs the KWin script and keeps it installed across a
// KWin restart, until ctx is done. This is the one part of this
// package that cannot be unit tested without a live KWin/D-Bus
// session; specs/milestones/M06-focus-tracking.md's Verification
// section covers the manual check (restart KWin, confirm reinstall).
func (p *kwinProvider) supervise(ctx context.Context, scriptPath string) {
	defer close(p.done)

	p.installScript(scriptPath)
	go p.warnIfNoHandshake(ctx)

	sig := make(chan *dbus.Signal, 8)
	p.conn.Signal(sig)
	defer p.conn.RemoveSignal(sig)

	if err := p.conn.AddMatchSignal(
		dbus.WithMatchSender("org.freedesktop.DBus"),
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, "org.kde.KWin"),
	); err != nil {
		p.log.Warn("focus: could not watch for KWin restarts; the script will not be reinstalled automatically if KWin restarts", "err", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case s, ok := <-sig:
			if !ok {
				return
			}
			if s.Name != "org.freedesktop.DBus.NameOwnerChanged" {
				continue
			}
			if len(s.Body) < 3 {
				continue
			}
			newOwner, _ := s.Body[2].(string)
			if newOwner == "" {
				continue // KWin went away, not (re)appeared
			}
			select {
			case <-time.After(kwinRestartReinstallDelay):
			case <-ctx.Done():
				return
			}
			p.log.Info("focus: KWin restarted; reinstalling the focus-tracking script")
			p.installScript(scriptPath)
		}
	}
}

// installScript loads and starts the KWin script, unloading any
// previous instance registered under the same plugin name first --
// mandatory, not hygiene: loadScript on an already-registered plugin
// name is a no-op that returns the existing id without re-reading the
// file (confirmed live, see scripting.go's loadScript doc comment), so
// a restarted daemon would otherwise keep running whatever script body
// a previous instance installed.
func (p *kwinProvider) installScript(scriptPath string) {
	kw := newKWinScripting(p.conn)
	if loaded, err := kw.isScriptLoaded(kwinPluginName); err == nil && loaded {
		if err := kw.unloadScript(kwinPluginName); err != nil {
			p.log.Warn("focus: unload previous KWin script failed", "err", err)
		}
	}
	if err := kw.loadScript(scriptPath, kwinPluginName); err != nil {
		p.log.Warn("focus: load KWin script failed; is KWin running on this session bus?", "path", scriptPath, "err", err)
		return
	}
	if err := kw.start(); err != nil {
		p.log.Warn("focus: start KWin script failed", "err", err)
	}
}

// warnIfNoHandshake logs once if no focus event -- not even the
// script's own unconditional load-time report, see
// script/knobd-focus.js -- has arrived within handshakeTimeout. See the
// package-level handshakeTimeout doc comment for why this, not
// loadScript's return value, is the signal this package trusts.
func (p *kwinProvider) warnIfNoHandshake(ctx context.Context) {
	select {
	case <-time.After(handshakeTimeout):
	case <-ctx.Done():
		return
	}
	p.mu.RLock()
	known := p.known
	p.mu.RUnlock()
	if !known {
		p.log.Warn("focus: no focus event received yet; is KWin running, and did the script actually load? debug with: journalctl --user -f | grep knobd-focus")
	}
}
