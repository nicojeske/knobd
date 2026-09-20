package api

import "fmt"

func errNotImplemented(fn string) error {
	return fmt.Errorf("api: %s is not implemented yet (see specs/milestones/M04-mapping-engine-daemon.md)", fn)
}
