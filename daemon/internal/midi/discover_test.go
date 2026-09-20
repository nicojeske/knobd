package midi

import (
	"os"
	"path/filepath"
	"testing"
)

// mkCharDeviceStandIn creates a plain regular file standing in for the
// rawmidi node: discovery only needs the path to exist (see discover.go
// — confirming it's actually a character device is Open's job, tested
// separately in rawmidi_test.go against a real regular file, since
// creating a real char-special device node needs root).
func mkCharDeviceStandIn(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDiscoverInByID(t *testing.T) {
	root := t.TempDir()
	byID := filepath.Join(root, "by-id")
	if err := os.MkdirAll(byID, 0o755); err != nil {
		t.Fatal(err)
	}

	// Card 3, matching the development machine's layout.
	mkCharDeviceStandIn(t, filepath.Join(root, "midiC3D0"))
	if err := os.Symlink("../controlC3", filepath.Join(byID, "usb-Behringer_X-TOUCH_MINI_1.0.1-00")); err != nil {
		t.Fatal(err)
	}
	// A second, unrelated by-id entry (this machine has 6 sound cards)
	// must not be picked up.
	mkCharDeviceStandIn(t, filepath.Join(root, "midiC5D0"))
	if err := os.Symlink("../controlC5", filepath.Join(byID, "usb-Some_Other_Device-00")); err != nil {
		t.Fatal(err)
	}

	infos, err := discoverIn(root)
	if err != nil {
		t.Fatalf("discoverIn: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("discoverIn() = %+v, want exactly 1 entry", infos)
	}
	want := DeviceInfo{
		Name:     "Behringer X-TOUCH MINI",
		Path:     filepath.Join(root, "midiC3D0"),
		StableID: filepath.Join(byID, "usb-Behringer_X-TOUCH_MINI_1.0.1-00"),
	}
	if infos[0] != want {
		t.Errorf("discoverIn() = %+v, want %+v", infos[0], want)
	}
}

func TestDiscoverInMultipleXTouchByIDEntries(t *testing.T) {
	root := t.TempDir()
	byID := filepath.Join(root, "by-id")
	os.MkdirAll(byID, 0o755)

	mkCharDeviceStandIn(t, filepath.Join(root, "midiC3D0"))
	os.Symlink("../controlC3", filepath.Join(byID, "usb-Behringer_X-TOUCH_MINI_1.0.1-00"))
	mkCharDeviceStandIn(t, filepath.Join(root, "midiC4D0"))
	os.Symlink("../controlC4", filepath.Join(byID, "usb-Behringer_X-TOUCH_MINI_1.0.1-01"))

	infos, err := discoverIn(root)
	if err != nil {
		t.Fatalf("discoverIn: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("discoverIn() = %+v, want 2 entries", infos)
	}
}

func TestDiscoverInDanglingSymlinkSkipped(t *testing.T) {
	root := t.TempDir()
	byID := filepath.Join(root, "by-id")
	os.MkdirAll(byID, 0o755)

	// Points at a controlC3 that doesn't exist and has no matching
	// midiC3D0 either — a stale entry from a device that's since fully
	// gone, not just its symlink.
	os.Symlink("../controlC3", filepath.Join(byID, "usb-Behringer_X-TOUCH_MINI_1.0.1-00"))

	infos, err := discoverIn(root)
	if err != nil {
		t.Fatalf("discoverIn: %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("discoverIn() = %+v, want none (dangling entry should be skipped, not fatal)", infos)
	}
}

func TestDiscoverInFallsBackWhenByIDMissing(t *testing.T) {
	root := t.TempDir()
	// No by-id directory at all (udev not running).
	mkCharDeviceStandIn(t, filepath.Join(root, "midiC3D0"))

	infos, err := discoverIn(root)
	if err != nil {
		t.Fatalf("discoverIn: %v", err)
	}
	if len(infos) != 1 || infos[0].Path != filepath.Join(root, "midiC3D0") {
		t.Fatalf("discoverIn() = %+v, want the fallback midiC3D0 entry", infos)
	}
	if infos[0].StableID != "" {
		t.Errorf("fallback entry StableID = %q, want empty (no by-id to resolve from)", infos[0].StableID)
	}
}

func TestDiscoverInNoDevice(t *testing.T) {
	root := t.TempDir()
	infos, err := discoverIn(root)
	if err != nil {
		t.Fatalf("discoverIn: %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("discoverIn() = %+v, want none", infos)
	}
}

// TestDiscoverRealDevice exercises the real Discover() against the
// actual development machine's /dev/snd. It's skipped wherever the
// X-Touch Mini isn't plugged in (CI, most machines) rather than failed,
// since M02's acceptance criterion for this is explicitly a by-hand
// check, not a CI gate.
func TestDiscoverRealDevice(t *testing.T) {
	infos, err := Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(infos) == 0 {
		t.Skip("no X-Touch Mini connected to this machine; skipping")
	}
	for _, info := range infos {
		fi, err := os.Stat(info.Path)
		if err != nil {
			t.Errorf("discovered path %s does not stat: %v", info.Path, err)
			continue
		}
		if fi.Mode()&os.ModeCharDevice == 0 {
			t.Errorf("discovered path %s is not a character device", info.Path)
		}
	}
}
