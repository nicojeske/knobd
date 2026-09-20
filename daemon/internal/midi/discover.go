package midi

import (
	"fmt"
	"os"
	"path/filepath"
)

// sndDir is the directory Discover scans in production. Tests override
// discovery entirely by calling discoverIn with a t.TempDir() in its
// place, since /dev/snd can't be faked in-process.
const sndDir = "/dev/snd"

// byIDGlob matches every by-id symlink Behringer's udev rules create for
// an X-Touch Mini, across firmware revisions (the "1.0.1" segment is a
// firmware version, confirmed via the by-id name on the development
// machine: usb-Behringer_X-TOUCH_MINI_1.0.1-00).
const byIDGlob = "usb-Behringer_X-TOUCH_MINI_*"

// Discover lists connected X-Touch Minis by scanning /dev/snd/by-id.
func Discover() ([]DeviceInfo, error) {
	return discoverIn(sndDir)
}

func discoverIn(root string) ([]DeviceInfo, error) {
	links, err := filepath.Glob(filepath.Join(root, "by-id", byIDGlob))
	if err != nil {
		return nil, fmt.Errorf("midi: glob %s/by-id: %w", root, err)
	}

	var infos []DeviceInfo
	for _, link := range links {
		info, err := resolveByID(root, link)
		if err != nil {
			// A stale or dangling by-id entry shouldn't fail discovery of
			// a device that's otherwise fine; just skip it.
			continue
		}
		infos = append(infos, info)
	}
	if len(infos) > 0 {
		return infos, nil
	}

	// by-id unavailable (e.g. udev not running). Fall back to any rawmidi
	// node directly; there's no vendor string to check outside by-id, so
	// these are reported with a generic name and Open's char-device check
	// is what catches an unrelated card.
	nodes, err := filepath.Glob(filepath.Join(root, "midiC*D0"))
	if err != nil {
		return nil, fmt.Errorf("midi: glob %s/midiC*D0: %w", root, err)
	}
	for _, n := range nodes {
		infos = append(infos, DeviceInfo{Name: "MIDI device (vendor unconfirmed, by-id unavailable)", Path: n})
	}
	return infos, nil
}

// resolveByID follows a /dev/snd/by-id/... symlink — which points at the
// *control* device (e.g. ../controlC3), not the rawmidi node — to the
// corresponding /dev/snd/midiC<N>D0. See port.go's package doc comment
// for why this indirection exists.
func resolveByID(root, link string) (DeviceInfo, error) {
	target, err := os.Readlink(link)
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("midi: readlink %s: %w", link, err)
	}

	var card int
	if _, err := fmt.Sscanf(filepath.Base(target), "controlC%d", &card); err != nil {
		return DeviceInfo{}, fmt.Errorf("midi: %s resolves to %q, not a controlCN device: %w", link, target, err)
	}

	// Confirming this is actually a character device is Open's job (it
	// has to do that check anyway, to fail fast rather than hang — see
	// rawmidi.go); discovery only needs to know the path exists.
	midiPath := filepath.Join(root, fmt.Sprintf("midiC%dD0", card))
	if _, err := os.Stat(midiPath); err != nil {
		return DeviceInfo{}, fmt.Errorf("midi: card %d has no rawmidi device at %s: %w", card, midiPath, err)
	}

	return DeviceInfo{
		Name:     "Behringer X-TOUCH MINI",
		Path:     midiPath,
		StableID: link,
	}, nil
}
