package focus

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed script/knobd-focus.js
var rawScript string

// renderScript substitutes the KWin script's D-Bus destination
// placeholders and returns the ready-to-write source. It fails loudly
// (rather than silently emitting a script that would call the wrong
// destination) if any placeholder survives substitution -- which would
// only happen if a future edit to script/knobd-focus.js added a new
// @…@ token this function doesn't know about.
func renderScript(service, object, iface string) (string, error) {
	out := rawScript
	out = strings.ReplaceAll(out, "@KNOBD_SERVICE@", service)
	out = strings.ReplaceAll(out, "@KNOBD_OBJECT@", object)
	out = strings.ReplaceAll(out, "@KNOBD_INTERFACE@", iface)
	// Checked against the three known tokens specifically, not a
	// "@KNOBD_" prefix scan: the script's own doc comment mentions the
	// placeholder syntax by name, which a broader check would flag as
	// unsubstituted.
	for _, ph := range []string{"@KNOBD_SERVICE@", "@KNOBD_OBJECT@", "@KNOBD_INTERFACE@"} {
		if strings.Contains(out, ph) {
			return "", fmt.Errorf("focus: rendered script still contains unsubstituted placeholder %s", ph)
		}
	}
	return out, nil
}

// scriptDir returns the directory materializeScript writes into:
// $XDG_RUNTIME_DIR/knobd if set (the normal case -- 0700 user-owned
// tmpfs, wiped on logout, readable by KWin since it runs as the same
// user), falling back to $TMPDIR/knobd-<uid> like api.SocketPath does
// for the same reason.
func scriptDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "knobd")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("knobd-%d", os.Getuid()))
}

// materializeScript renders the script and writes it to dir/knobd-focus.js,
// returning the path KWin should load. It writes to a temp file in the
// same directory and renames into place, so a concurrent KWin
// loadScript call (a prior daemon instance still shutting down, or a
// retry) can never observe a half-written file.
func materializeScript(dir, service, object, iface string) (string, error) {
	src, err := renderScript(service, object, iface)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("focus: create script dir %s: %w", dir, err)
	}

	final := filepath.Join(dir, "knobd-focus.js")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, []byte(src), 0o600); err != nil {
		return "", fmt.Errorf("focus: write script: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("focus: install script at %s: %w", final, err)
	}
	return final, nil
}
