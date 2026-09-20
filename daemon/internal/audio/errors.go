package audio

import "fmt"

func errNotImplemented(fn string) error {
	return fmt.Errorf("audio: %s is not implemented yet (see specs/milestones/M03-audio-control.md)", fn)
}
