package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// CurrentSchemaVersion is the schemaVersion value written to state.json.
// Increment on any incompatible on-disk layout change.
const CurrentSchemaVersion = 1

const stateFileName = "state.json"

func newInstanceUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Load reads <dir>/state.json. When absent, a fresh default state is written and
// returned. When schemaVersion does not match CurrentSchemaVersion the call
// returns an error containing "schemaVersion" rather than silently migrating.
func Load(dir string) (*State, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir state dir: %w", err)
	}
	path := filepath.Join(dir, stateFileName)
	b, err := os.ReadFile(path) //nolint:gosec // path is derived from caller-controlled data dir, not request input
	if errors.Is(err, os.ErrNotExist) {
		uuid, err := newInstanceUUID()
		if err != nil {
			return nil, err
		}
		st := NewDefault(uuid)
		st.dir = dir
		if err := st.Save(); err != nil {
			return nil, err
		}
		return st, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state.json: %w", err)
	}

	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("parse state.json: %w", err)
	}
	if st.SchemaVersion != CurrentSchemaVersion {
		return nil, fmt.Errorf(
			"state.json schemaVersion %d does not match expected %d; refusing to auto-migrate",
			st.SchemaVersion, CurrentSchemaVersion,
		)
	}
	if st.PairedPeers == nil {
		st.PairedPeers = map[string]PairedPeer{}
	}
	if st.LLMProviders == nil {
		st.LLMProviders = map[string]LLMProviderMeta{}
	}
	st.dir = dir
	st.mu = &sync.RWMutex{}
	return &st, nil
}

// Mutate calls fn while holding the write lock. fn must not panic (panics leave
// the mutex locked). Do not call Save inside fn.
func (s *State) Mutate(fn func(*State)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(s)
}

// Dir returns the directory that state was loaded from. Useful for co-locating
// sockets, certs, and log files next to state.json.
func (s *State) Dir() string { return s.dir }

// InstanceUUID returns the stable daemon identity UUID written to state.json
// on first boot. Safe for concurrent reads.
func (s *State) InstanceUUID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.DaemonInstanceUUID
}

// Snapshot returns a deep-ish copy safe for read-only callers. Maps are
// shallow-copied since their value types are immutable structs in this
// codebase (PairedPeer, LLMProviderMeta) — callers must not mutate the
// returned maps in place. The returned value's internal mutex is nil; calling
// Mutate or Save on a snapshot will panic by design (snapshots are read-only).
func (s *State) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := *s
	// Drop the lock pointer on the copy so the snapshot cannot share the
	// live state's mutex. Snapshots are strictly read-only data bags.
	out.mu = nil
	out.PairedPeers = make(map[string]PairedPeer, len(s.PairedPeers))
	for k, v := range s.PairedPeers {
		out.PairedPeers[k] = v
	}
	out.LLMProviders = make(map[string]LLMProviderMeta, len(s.LLMProviders))
	for k, v := range s.LLMProviders {
		out.LLMProviders[k] = v
	}
	return out
}

// Save writes state.json atomically via a tmp-file + rename. Permissions: 0o600.
func (s *State) Save() error {
	if s.dir == "" {
		return errors.New("state: dir not bound; load via Load(dir) first")
	}
	s.mu.RLock()
	b, err := json.MarshalIndent(s, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, stateFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
