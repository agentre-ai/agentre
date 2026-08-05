package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_SettingsEnvironment(t *testing.T) {
	t.Run("Given inline settings and provider environment, when a stream runs, then a private merged settings file is used and removed", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		const gatewayCredential = "gateway-test-token"
		var settingsPath string
		captureSpawner := func(c *Client) {
			c.spawner = func(ctx context.Context, spec processSpec) (*process, error) {
				settingsPath = argValue(spec.args, string(flagSettings))
				require.NotEmpty(t, settingsPath)
				assert.NotContains(t, strings.Join(spec.args, " "), gatewayCredential)

				info, err := os.Stat(settingsPath)
				require.NoError(t, err)
				assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
				data, err := os.ReadFile(settingsPath) //nolint:gosec // test-owned temporary settings
				require.NoError(t, err)
				var settings struct {
					Hooks map[string]any    `json:"hooks"`
					Env   map[string]string `json:"env"`
				}
				require.NoError(t, json.Unmarshal(data, &settings))
				assert.Contains(t, settings.Hooks, "Stop")
				assert.Equal(t, "preserved", settings.Env["EXISTING_SETTING"])
				assert.Equal(t, "http://gateway.test", settings.Env["ANTHROPIC_BASE_URL"])
				assert.Equal(t, gatewayCredential, settings.Env["ANTHROPIC_AUTH_TOKEN"])
				return newPipeProcess(t, ctx, fakeStreamHappy), nil
			}
		}

		c := New(
			WithSettings(`{"hooks":{"Stop":[]},"env":{"EXISTING_SETTING":"preserved","ANTHROPIC_BASE_URL":"http://user-setting.invalid"}}`),
			WithSettingsEnv(map[string]string{
				"ANTHROPIC_BASE_URL":   "http://gateway.test",
				"ANTHROPIC_AUTH_TOKEN": gatewayCredential,
			}),
			captureSpawner,
		)
		stream, err := c.Stream(ctx, "hello")
		require.NoError(t, err)
		for stream.Next() {
		}
		require.NoError(t, stream.Close(ctx))
		_, err = os.Stat(settingsPath)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("Given provider environment, when spawning fails, then the temporary settings file is removed", func(t *testing.T) {
		var settingsPath string
		spawnErr := errors.New("fake spawn failed")
		c := New(
			WithSettingsEnv(map[string]string{"ANTHROPIC_AUTH_TOKEN": "gateway-test-token"}),
			func(c *Client) {
				c.spawner = func(_ context.Context, spec processSpec) (*process, error) {
					settingsPath = argValue(spec.args, string(flagSettings))
					require.FileExists(t, settingsPath)
					return nil, spawnErr
				}
			},
		)

		_, err := c.Stream(context.Background(), "hello")
		assert.ErrorIs(t, err, spawnErr)
		_, err = os.Stat(settingsPath)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("Given file settings and provider environment, when a session closes, then settings are merged and the temporary copy is removed", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		dir := t.TempDir()
		originalPath := filepath.Join(dir, "settings.json")
		require.NoError(t, os.WriteFile(originalPath, []byte(`{"permissions":{"defaultMode":"acceptEdits"}}`), 0o600))

		var settingsPath string
		blockingCLI := func(stdin io.Reader, _ io.Writer) {
			_, _ = io.Copy(io.Discard, stdin)
		}
		c := New(
			WithSettings(originalPath),
			WithSettingsEnv(map[string]string{"ANTHROPIC_BASE_URL": "http://gateway.test"}),
			func(c *Client) {
				c.spawner = func(ctx context.Context, spec processSpec) (*process, error) {
					settingsPath = argValue(spec.args, string(flagSettings))
					require.NotEqual(t, originalPath, settingsPath)
					require.FileExists(t, settingsPath)
					return newPipeProcess(t, ctx, blockingCLI), nil
				}
			},
		)

		session, err := c.OpenSession(ctx)
		require.NoError(t, err)
		require.NoError(t, session.Close(ctx))
		_, err = os.Stat(settingsPath)
		assert.ErrorIs(t, err, os.ErrNotExist)
		require.FileExists(t, originalPath)
	})
}

func argValue(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}
