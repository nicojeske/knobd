package focus

import (
	"errors"
	"fmt"
)

func errNotImplemented(fn string) error {
	return fmt.Errorf("focus: %s is not implemented yet (see specs/milestones/M06-focus-tracking.md)", fn)
}

// ErrUnavailable is returned by Unavailable's Provider from Current, and
// is what a TargetFocused resolution or knob.assign_focused_app handler
// sees until M06 lands a real Provider.
var ErrUnavailable = errors.New("focus: focus tracking is not available (see specs/milestones/M06-focus-tracking.md)")
