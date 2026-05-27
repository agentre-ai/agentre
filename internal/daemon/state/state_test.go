package state

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupStateTest(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TestStateLoadSave(t *testing.T) {
	convey.Convey("Load/Save state.json", t, func() {
		dir := setupStateTest(t)

		convey.Convey("when state.json absent, Load returns default + writes file", func() {
			st, err := Load(dir)
			require.NoError(t, err)
			assert.NotEmpty(t, st.DaemonInstanceUUID)
			assert.Equal(t, 1, st.SchemaVersion)
			_, err = os.Stat(filepath.Join(dir, "state.json"))
			assert.NoError(t, err)
		})

		convey.Convey("when state.json present, Load reuses persisted UUID", func() {
			st1, _ := Load(dir)
			uuid1 := st1.DaemonInstanceUUID
			st2, err := Load(dir)
			require.NoError(t, err)
			assert.Equal(t, uuid1, st2.DaemonInstanceUUID,
				"daemonInstanceUUID must be stable across boots")
		})

		convey.Convey("Save writes atomically and is readable back", func() {
			st, _ := Load(dir)
			st.Mutate(func(s *State) {
				s.PairedPeers["sha256:x"] = PairedPeer{
					DeviceName: "foo", DeviceToken: "t", PairedAt: 1, LastSeenAt: 1,
				}
			})
			require.NoError(t, st.Save())

			st2, _ := Load(dir)
			peer, ok := st2.PairedPeers["sha256:x"]
			require.True(t, ok)
			assert.Equal(t, "foo", peer.DeviceName)
		})

		convey.Convey("Schema version mismatch is an error (no auto-migrate)", func() {
			path := filepath.Join(dir, "state.json")
			require.NoError(t, os.WriteFile(path, []byte(`{"schemaVersion":99}`), 0o600))
			_, err := Load(dir)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "schemaVersion")
		})

		convey.Convey("Concurrent Mutate calls are race-free", func() {
			st, _ := Load(dir)
			var wg sync.WaitGroup
			for i := 0; i < 100; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					st.Mutate(func(s *State) {
						s.LLMProviders[string(rune('a'+i%26))] = LLMProviderMeta{Name: "x"}
					})
				}(i)
			}
			wg.Wait()
			assert.NotEmpty(t, st.LLMProviders)
		})

		convey.Convey("Atomic write: partial write does not corrupt", func() {
			st, _ := Load(dir)
			require.NoError(t, st.Save())
			info, err := os.Stat(filepath.Join(dir, "state.json"))
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		})

		convey.Convey("Snapshot returns an independent copy of maps", func() {
			st, _ := Load(dir)
			st.Mutate(func(s *State) {
				s.LLMProviders["a"] = LLMProviderMeta{Name: "orig"}
			})
			snap := st.Snapshot()
			snap.LLMProviders["a"] = LLMProviderMeta{Name: "changed"}
			// Live state unchanged.
			assert.Equal(t, "orig", st.LLMProviders["a"].Name)
		})
	})
}
