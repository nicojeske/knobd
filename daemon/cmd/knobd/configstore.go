package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/njeske/knobd/internal/config"
	"github.com/njeske/knobd/internal/model"
)

// configApplier is the slice of *engine.Engine configStore needs -- a
// point-of-use interface so configStore is testable without a real
// engine, a real MIDI port, or PipeWire.
type configApplier interface {
	SetConfig(ctx context.Context, cfg model.Config) error
}

// configStore is cmd/knobd's api.ConfigStore adapter (see
// daemon/internal/api's ConfigStore interface): it owns the in-memory
// config, the running engine, and the on-disk file, keeping all three in
// sync. It is also the single entry point PUT /config, a SIGHUP reload,
// and a device-triggered mutation (knob.assign_focused_app, via
// actions.ConfigMutator) all go through.
type configStore struct {
	mu   sync.RWMutex
	path string
	cfg  model.Config
	// rev counts every successful SetConfig, starting at 0 for the
	// as-loaded config passed to newConfigStore. onChanged reports it so
	// a subscriber (the SSE hub) can notice a config change without
	// receiving the config itself -- see onChanged's doc comment.
	rev uint64
	eng configApplier
	log *slog.Logger
	// onChanged, if non-nil, is called after a SetConfig that fully
	// succeeded (the engine applied it AND it was durably saved) --
	// never on a rollback. Called OUTSIDE s.mu: broadcasting under the
	// config lock would let a slow subscriber (GET /events' hub) stall
	// the next PUT /config, which must not happen.
	onChanged func(rev uint64)
}

func newConfigStore(path string, cfg model.Config, eng configApplier, onChanged func(rev uint64), log *slog.Logger) *configStore {
	return &configStore{path: path, cfg: cfg, eng: eng, onChanged: onChanged, log: log}
}

// Config implements api.ConfigStore.
func (s *configStore) Config() model.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// SetConfig implements api.ConfigStore. It is engine-first, disk-second:
// the engine swap is the only one of the two steps that can be undone,
// so on a save failure it rolls the engine back to the previous config
// rather than leaving disk and the running engine disagreeing --
// config.Save's temp-file-plus-rename already makes the disk write
// itself atomic. There is a sub-millisecond window where a knob turned
// between the engine swap and a failing save would briefly use the new
// config before the rollback lands; accepted as self-healing rather than
// worth a bigger transaction for.
func (s *configStore) SetConfig(ctx context.Context, cfg model.Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("configstore: %w", err)
	}

	rev, err := s.apply(ctx, cfg)
	if err != nil {
		return err
	}

	// Fires outside s.mu (apply above already released it) -- see
	// onChanged's doc comment for why.
	if s.onChanged != nil {
		s.onChanged(rev)
	}
	return nil
}

// apply does the locked engine-first/disk-second swap and returns the
// new revision on success. Split out from SetConfig so the lock's scope
// is visibly confined to this function and can never accidentally grow
// to cover the onChanged call above.
func (s *configStore) apply(ctx context.Context, cfg model.Config) (rev uint64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	previous := s.cfg
	if err := s.eng.SetConfig(ctx, cfg); err != nil {
		return 0, fmt.Errorf("configstore: apply to engine: %w", err)
	}
	if err := config.Save(s.path, cfg); err != nil {
		if rbErr := s.eng.SetConfig(ctx, previous); rbErr != nil {
			s.log.Error("configstore: failed to roll the engine back after a failed save", "err", rbErr)
		}
		return 0, fmt.Errorf("configstore: save: %w", err)
	}
	s.cfg = cfg
	s.rev++
	return s.rev, nil
}
