package focus

import "fmt"

func errNotImplemented(fn string) error {
	return fmt.Errorf("focus: %s is not implemented yet (see specs/milestones/M06-focus-tracking.md)", fn)
}
