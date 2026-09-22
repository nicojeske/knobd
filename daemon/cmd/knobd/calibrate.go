package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/njeske/knobd/internal/midi"
)

// runCalibrateLEDs implements `knobd calibrate-leds`: it sends one raw
// LED-related MIDI message to the X-Touch Mini and exits, leaving the
// device's own latched LED state as the observable result. See
// specs/milestones/M05-led-feedback.md's calibration step and
// specs/reference/xtouch-mini-midi-map.md's LED section, which this
// command exists to empirically confirm before device.Codec.EncodeLED
// is implemented against it.
//
// It is deliberately one-shot rather than an interactive sweep: LED
// state on this device latches (it stays lit until the next write, it
// does not need to be held), so each invocation sends exactly one
// message and returns, and the caller decides the pacing -- one
// invocation per value while watching the physical unit, or a small
// shell loop for a full sweep. This also makes calibration scriptable
// and replayable, unlike a program that blocks on stdin.
func runCalibrateLEDs(args []string) error {
	fs := flag.NewFlagSet("calibrate-leds", flag.ContinueOnError)
	devicePath := fs.String("device", "", "open this rawmidi path directly instead of discovering one")
	logLevel := fs.String("log-level", "warn", "log level for the underlying connection: debug, info, warn, error")
	channel := fs.Int("channel", 1, "1-based MIDI channel to send on (the documented LED encoding uses channel 1)")
	cc := fs.Int("cc", -1, "send a Control Change on this controller number (e.g. 48 for encoder 1's ring) -- use with -value")
	value := fs.Int("value", -1, "the CC value to send, 0-127 -- use with -cc")
	note := fs.Int("note", -1, "send a Note On on this note number (e.g. 89 for button 1) -- use with -velocity")
	velocity := fs.Int("velocity", -1, "the Note On velocity to send, 0-127 (0=off, 1=on, 2=blinking per the documented encoding) -- use with -note")
	timeout := fs.Duration("timeout", 5*time.Second, "how long to wait for the device to connect before giving up")
	if err := fs.Parse(args); err != nil {
		return err
	}

	haveCC := *cc >= 0 || *value >= 0
	haveNote := *note >= 0 || *velocity >= 0
	switch {
	case haveCC && haveNote:
		return fmt.Errorf("calibrate-leds: pass either -cc/-value or -note/-velocity, not both")
	case haveCC:
		if *cc < 0 || *cc > 127 || *value < 0 || *value > 127 {
			return fmt.Errorf("calibrate-leds: -cc and -value must both be set, in range 0-127")
		}
	case haveNote:
		if *note < 0 || *note > 127 || *velocity < 0 || *velocity > 127 {
			return fmt.Errorf("calibrate-leds: -note and -velocity must both be set, in range 0-127")
		}
	default:
		return fmt.Errorf("calibrate-leds: nothing to send -- pass -cc/-value (a ring) or -note/-velocity (a button)")
	}
	if *channel < 1 || *channel > 16 {
		return fmt.Errorf("calibrate-leds: -channel must be 1-16")
	}

	level := slog.LevelWarn
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		return fmt.Errorf("invalid -log-level %q: %w", *logLevel, err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	opts := midi.SupervisorOptions{Logger: logger}
	if *devicePath != "" {
		path := *devicePath
		opts.Discover = func() ([]midi.DeviceInfo, error) {
			return []midi.DeviceInfo{{Name: path, Path: path}}, nil
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sup := midi.NewSupervisor(opts)
	defer sup.Close()

	connectCtx, cancelConnect := context.WithTimeout(ctx, *timeout)
	defer cancelConnect()
	select {
	case info := <-sup.Connected():
		fmt.Printf("connected: %s (%s)\n", info.Name, info.Path)
	case <-connectCtx.Done():
		return fmt.Errorf("calibrate-leds: no X-Touch Mini found within %s", *timeout)
	}

	var msg midi.Message
	switch {
	case haveCC:
		msg = midi.Message{Status: 0xB0 | byte(*channel-1), Data1: byte(*cc), Data2: byte(*value)}
		fmt.Printf("sending CC %d = %d (0x%02X) on channel %d -- status=0x%02X data1=0x%02X data2=0x%02X\n",
			*cc, *value, *value, *channel, msg.Status, msg.Data1, msg.Data2)
	case haveNote:
		msg = midi.Message{Status: 0x90 | byte(*channel-1), Data1: byte(*note), Data2: byte(*velocity)}
		fmt.Printf("sending Note On %d velocity %d on channel %d -- status=0x%02X data1=0x%02X data2=0x%02X\n",
			*note, *velocity, *channel, msg.Status, msg.Data1, msg.Data2)
	}

	writeCtx, cancelWrite := context.WithTimeout(ctx, 2*time.Second)
	defer cancelWrite()
	if err := sup.Write(writeCtx, msg); err != nil {
		return fmt.Errorf("calibrate-leds: write: %w", err)
	}
	fmt.Println("sent. describe what changed on the physical unit.")
	return nil
}
