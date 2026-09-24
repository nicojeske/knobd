package spotify

import "sync"

// FakeSecretStore is an in-memory SecretStore for tests -- no D-Bus,
// no real Secret Service required. See internal/media's FakeBackend for
// the same pattern applied to a different hardware-facing interface.
type FakeSecretStore struct {
	mu     sync.Mutex
	tokens map[string]string
}

// NewFakeSecretStore returns an empty FakeSecretStore.
func NewFakeSecretStore() *FakeSecretStore {
	return &FakeSecretStore{tokens: make(map[string]string)}
}

func (f *FakeSecretStore) Get(account string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tok, ok := f.tokens[account]
	return tok, ok, nil
}

func (f *FakeSecretStore) Set(account, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens[account] = token
	return nil
}

func (f *FakeSecretStore) Delete(account string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.tokens, account)
	return nil
}

var _ SecretStore = (*FakeSecretStore)(nil)
