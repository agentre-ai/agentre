package pairing

import (
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPairingCodeFormat(t *testing.T) {
	c, _ := NewCode()
	assert.Len(t, c, 6)
	const forbidden = "01OI"
	for _, ch := range c {
		assert.NotContains(t, forbidden, string(ch))
	}
}

func TestPairingManager(t *testing.T) {
	convey.Convey("PairingManager.Generate + Consume", t, func() {
		clock := newFakeClock(time.Unix(1716_000_000, 0))
		m := NewManager(ManagerOpts{
			TTL:   5 * time.Minute,
			Clock: clock.Now,
		})

		convey.Convey("Generate returns a 6-char code; Consume succeeds once", func() {
			code, err := m.Generate()
			require.NoError(t, err)
			require.Len(t, code, 6)

			ok := m.Consume(code)
			assert.True(t, ok)

			ok2 := m.Consume(code)
			assert.False(t, ok2, "code must be one-shot")
		})

		convey.Convey("Code expires after TTL", func() {
			code, _ := m.Generate()
			clock.Advance(6 * time.Minute)
			assert.False(t, m.Consume(code))
		})

		convey.Convey("Unknown code never matches", func() {
			assert.False(t, m.Consume("ZZZZZZ"))
		})

		convey.Convey("Generate is overwriteable: only latest valid", func() {
			c1, _ := m.Generate()
			c2, _ := m.Generate()
			assert.False(t, m.Consume(c1), "stale code invalidated by Generate()")
			assert.True(t, m.Consume(c2))
		})
	})
}

func TestRateLimit(t *testing.T) {
	clock := newFakeClock(time.Unix(1716_000_000, 0))
	rl := NewRateLimiter(RateLimitOpts{
		MaxAttempts: 3,
		Window:      60 * time.Second,
		Clock:       clock.Now,
	})

	for i := 0; i < 3; i++ {
		assert.True(t, rl.Allow("1.2.3.4"), "attempt %d", i+1)
	}
	assert.False(t, rl.Allow("1.2.3.4"))

	clock.Advance(61 * time.Second)
	assert.True(t, rl.Allow("1.2.3.4"))

	assert.True(t, rl.Allow("5.6.7.8"))
}

type fakeClock struct{ t time.Time }

func newFakeClock(t time.Time) *fakeClock    { return &fakeClock{t: t} }
func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }
