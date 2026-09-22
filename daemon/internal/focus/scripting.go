package focus

import "github.com/godbus/dbus/v5"

const (
	kwinService     = "org.kde.KWin"
	kwinScriptPath  = dbus.ObjectPath("/Scripting")
	kwinScriptIface = "org.kde.kwin.Scripting"
)

// kwinScripting is a thin, typed wrapper over org.kde.KWin's Scripting
// D-Bus interface -- just the handful of methods kwin.go needs
// (confirmed present via `qdbus6 org.kde.KWin /Scripting` during M06
// planning: isScriptLoaded, loadScript, unloadScript, start).
type kwinScripting struct {
	obj dbus.BusObject
}

func newKWinScripting(conn *dbus.Conn) kwinScripting {
	return kwinScripting{obj: conn.Object(kwinService, kwinScriptPath)}
}

func (k kwinScripting) isScriptLoaded(name string) (bool, error) {
	var loaded bool
	err := k.obj.Call(kwinScriptIface+".isScriptLoaded", 0, name).Store(&loaded)
	return loaded, err
}

func (k kwinScripting) unloadScript(name string) error {
	var ok bool
	return k.obj.Call(kwinScriptIface+".unloadScript", 0, name).Store(&ok)
}

// loadScript loads path under plugin name name.
//
// M06 finding, confirmed live (see
// specs/adr/0003-focus-tracking-via-kwin-script.md's "Resolved"
// section): loadScript's returned id is NOT a reliable failure signal.
// Given a nonexistent path, it returned a plugin id rather than the
// documented failure sentinel, and isScriptLoaded for that plugin name
// subsequently reported true. Deliberately not checking the returned
// id here for that reason -- the only trustworthy liveness signal this
// package has is the script's own load-time handshake report (see
// kwin.go's warnIfNoHandshake).
func (k kwinScripting) loadScript(path, name string) error {
	var id int32
	return k.obj.Call(kwinScriptIface+".loadScript", 0, path, name).Store(&id)
}

func (k kwinScripting) start() error {
	return k.obj.Call(kwinScriptIface+".start", 0).Store()
}
