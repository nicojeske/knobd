package midi

import "fmt"

// errNotImplemented marks scaffolding left for a specific milestone to
// fill in. It exists so that calling a not-yet-built entry point fails
// loudly and immediately, with a pointer to what to implement, instead
// of silently doing nothing.
func errNotImplemented(fn string) error {
	return fmt.Errorf("midi: %s is not implemented yet (see specs/milestones/M02-midi-transport.md)", fn)
}
