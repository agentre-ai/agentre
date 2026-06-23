package pairing

import (
	"crypto/rand"
	"sync"
	"time"
)

// CodeAlphabet excludes 0/O/1/I to avoid OCR/transcription confusion.
const CodeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

const CodeLen = 6

// NewCode returns a freshly generated 6-character code (~30 bits of entropy).
func NewCode() (string, error) {
	var b [CodeLen]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	out := make([]byte, CodeLen)
	for i, x := range b {
		out[i] = CodeAlphabet[int(x)%len(CodeAlphabet)]
	}
	return string(out), nil
}

// ManagerOpts wires the pairing manager.
type ManagerOpts struct {
	TTL   time.Duration
	Clock func() time.Time
}

// Manager holds a single pending pairing code at a time. Regenerate replaces
// the prior pending code (only the latest is valid). Consume is one-shot.
type Manager struct {
	mu        sync.Mutex
	opts      ManagerOpts
	pending   string
	expiresAt time.Time
}

func NewManager(opts ManagerOpts) *Manager {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.TTL == 0 {
		opts.TTL = 5 * time.Minute
	}
	return &Manager{opts: opts}
}

func (m *Manager) Generate() (string, error) {
	c, err := NewCode()
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = c
	m.expiresAt = m.opts.Clock().Add(m.opts.TTL)
	return c, nil
}

// Consume returns true exactly once for a matching, unexpired code.
func (m *Manager) Consume(code string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == "" || code != m.pending {
		return false
	}
	if m.opts.Clock().After(m.expiresAt) {
		m.pending = ""
		return false
	}
	m.pending = ""
	return true
}
