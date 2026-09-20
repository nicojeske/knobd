package device

import "fmt"

func errNotImplemented(fn string) error {
	return fmt.Errorf("device: %s is not implemented yet (see specs/milestones/M02-midi-transport.md)", fn)
}
