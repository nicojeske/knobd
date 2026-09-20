package engine

import "fmt"

func errNotImplemented(fn string) error {
	return fmt.Errorf("engine: %s is not implemented yet (see specs/milestones/M04-mapping-engine-daemon.md)", fn)
}
