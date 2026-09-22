package focus

import "encoding/json"

// scriptEvent mirrors the JSON object script/knobd-focus.js's report()
// sends as callDBus's single string argument. Every field has a
// deliberate zero-value fallback in the script itself, so a missing key
// here (an older/newer script, or a hand-crafted payload during
// testing) decodes to the same zero value encoding/json would give it
// anyway -- there is nothing this type needs to reject for a missing
// key, only for genuinely malformed JSON.
type scriptEvent struct {
	V             int    `json:"v"`
	Normal        bool   `json:"normal"`
	ResourceClass string `json:"resourceClass"`
	DesktopFileID string `json:"desktopFileId"`
	Caption       string `json:"caption"`
	PID           int    `json:"pid"`
}

// decodeScriptEvent parses one callDBus payload. It is the only place
// in this package that ever sees the script's raw JSON.
func decodeScriptEvent(payload string) (scriptEvent, error) {
	var ev scriptEvent
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return scriptEvent{}, err
	}
	return ev, nil
}

// ignoredResourceClasses filters out windows that are real (pass
// acceptEvent's normalWindow check) but never useful as a
// knob.assign_focused_app target -- shell chrome the user is never
// trying to route audio through. Small and empirically grown, like
// matcher.go's normalization allow-lists; validate/extend this against
// what actually activates on a real session (specs/milestones/M06's
// live-verification pass), not by guessing further ones.
// TODO(M07): make this configurable once there's a UI to edit it in.
var ignoredResourceClasses = map[string]bool{
	"plasmashell":  true,
	"krunner":      true,
	"kwin_wayland": true,
}

// acceptEvent reports whether ev describes a window worth reporting as
// "focused" at all -- called before appInfo() converts it. A null
// window (KWin reports none activated), a non-normal window (panels,
// docks, the like -- w.normalWindow from the script), or an explicitly
// ignored shell resourceClass are all rejected here rather than left
// for resolveFocused to quietly match nothing against: rejecting
// early means Provider.Current keeps returning the last REAL focused
// app instead of flickering to empty whenever focus passes through
// krunner.
func acceptEvent(ev scriptEvent) bool {
	if !ev.Normal {
		return false
	}
	if ev.ResourceClass == "" && ev.DesktopFileID == "" {
		return false
	}
	if ignoredResourceClasses[ev.ResourceClass] {
		return false
	}
	return true
}

// appInfo converts an accepted scriptEvent into the AppInfo shape the
// rest of the package uses. Binary is left unset here -- filling it
// needs a /proc/<pid>/exe read, done by the caller (kwin.go), which
// also knows the ProcRoot to use.
func (ev scriptEvent) appInfo() AppInfo {
	return AppInfo{
		PID:           ev.PID,
		ResourceClass: ev.ResourceClass,
		DesktopFileID: ev.DesktopFileID,
		Caption:       ev.Caption,
	}
}
