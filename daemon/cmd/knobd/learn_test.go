package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/njeske/knobd/internal/engine"
)

type fakeLearnEngine struct {
	gotDeadline time.Time
	err         error
}

func (f *fakeLearnEngine) SetLearnUntil(_ context.Context, deadline time.Time) error {
	f.gotDeadline = deadline
	return f.err
}

func TestLearnControllerStartLearnClamping(t *testing.T) {
	fixedNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name        string
		timeout     time.Duration
		wantTimeout time.Duration
	}{
		{"zero uses the engine default", 0, engine.DefaultLearnTimeout},
		{"negative uses the engine default", -1 * time.Second, engine.DefaultLearnTimeout},
		{"within range is passed through", 10 * time.Second, 10 * time.Second},
		{"above the max is clamped", 10 * time.Minute, engine.MaxLearnTimeout},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng := &fakeLearnEngine{}
			c := newLearnController(eng)
			c.now = func() time.Time { return fixedNow }

			state, err := c.StartLearn(context.Background(), tc.timeout)
			if err != nil {
				t.Fatalf("StartLearn: %v", err)
			}
			if !state.Active {
				t.Error("expected Active to be true")
			}
			wantDeadline := fixedNow.Add(tc.wantTimeout)
			if !eng.gotDeadline.Equal(wantDeadline) {
				t.Errorf("deadline passed to engine = %v, want %v", eng.gotDeadline, wantDeadline)
			}
			if state.ExpiresAt == nil || !state.ExpiresAt.Equal(wantDeadline) {
				t.Errorf("state.ExpiresAt = %v, want %v", state.ExpiresAt, wantDeadline)
			}
		})
	}
}

func TestLearnControllerStartLearnPropagatesError(t *testing.T) {
	eng := &fakeLearnEngine{err: errors.New("run not consuming")}
	c := newLearnController(eng)
	_, err := c.StartLearn(context.Background(), 0)
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestLearnControllerStopLearnDisarms(t *testing.T) {
	eng := &fakeLearnEngine{}
	c := newLearnController(eng)
	if err := c.StopLearn(context.Background()); err != nil {
		t.Fatalf("StopLearn: %v", err)
	}
	if !eng.gotDeadline.IsZero() {
		t.Errorf("deadline passed to engine = %v, want the zero time.Time", eng.gotDeadline)
	}
}
