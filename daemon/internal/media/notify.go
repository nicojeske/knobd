package media

import (
	"sync"

	"github.com/godbus/dbus/v5"
)

const (
	notifyBusName    = "org.freedesktop.Notifications"
	notifyObjectPath = dbus.ObjectPath("/org/freedesktop/Notifications")
	notifyIface      = "org.freedesktop.Notifications"
)

// dbusNotifier is the real, org.freedesktop.Notifications-backed
// Notifier -- confirmed present (served by plasmashell) during M09
// planning. It remembers the id its own last notification returned so a
// repeated media.now_playing press replaces that bubble (via
// Notify's replaces_id parameter) instead of piling up a new one.
type dbusNotifier struct {
	conn *dbus.Conn

	mu     sync.Mutex
	lastID uint32
}

// NewNotifier returns a Notifier that shows knobd's media.now_playing
// notifications over conn. conn is never closed by this Notifier -- its
// lifecycle belongs to whatever dialed it (media.New's connection, in
// cmd/knobd's wiring).
func NewNotifier(conn *dbus.Conn) Notifier {
	return &dbusNotifier{conn: conn}
}

func (n *dbusNotifier) Notify(summary, body string) error {
	n.mu.Lock()
	replaces := n.lastID
	n.mu.Unlock()

	obj := n.conn.Object(notifyBusName, notifyObjectPath)
	call := obj.Call(notifyIface+".Notify", 0,
		"knobd", replaces, "media-playback-start",
		summary, body,
		[]string{}, map[string]dbus.Variant{}, int32(5000),
	)
	if call.Err != nil {
		return call.Err
	}

	var id uint32
	if err := call.Store(&id); err != nil {
		return err
	}
	n.mu.Lock()
	n.lastID = id
	n.mu.Unlock()
	return nil
}

var _ Notifier = (*dbusNotifier)(nil)
