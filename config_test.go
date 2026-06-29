package cronometer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigRoundTrip(T *testing.T) {
	T.Parallel()

	T.Run("save then load", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "nested", "config.json")
		want := &Config{Email: "a@b.com", Password: "hunter2"}

		require.NoError(t, SaveConfig(path, want))

		got, err := LoadConfig(path)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	T.Run("writes 0600 permissions", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, SaveConfig(path, &Config{Email: "a@b.com", Password: "x"}))

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})

	T.Run("load missing file errors", func(t *testing.T) {
		t.Parallel()
		_, err := LoadConfig(filepath.Join(t.TempDir(), "absent.json"))
		require.Error(t, err)
	})
}

// TestLoadConfigExpandsEnv uses t.Setenv, so it cannot be parallel (the runtime walks up to the
// parent), and runs serially.
func TestLoadConfigExpandsEnv(T *testing.T) {
	T.Run("expands $VAR and ${VAR} in fields", func(t *testing.T) {
		t.Setenv("CRONO_TEST_PW", "from-env")
		t.Setenv("CRONO_TEST_TOTP", "SECRET32")

		path := filepath.Join(t.TempDir(), "config.json")
		raw := `{"email":"me@example.com","password":"$CRONO_TEST_PW","totp_secret":"${CRONO_TEST_TOTP}"}`
		require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))

		got, err := LoadConfig(path)
		require.NoError(t, err)
		assert.Equal(t, "me@example.com", got.Email)
		assert.Equal(t, "from-env", got.Password)
		assert.Equal(t, "SECRET32", got.TOTPSecret)
	})

	T.Run("undefined variable expands to empty", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		raw := `{"email":"me@example.com","password":"$CRONO_TEST_UNDEFINED"}`
		require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))

		got, err := LoadConfig(path)
		require.NoError(t, err)
		assert.Empty(t, got.Password)
	})
}

func TestResolveCredentialsFromFile(T *testing.T) {
	T.Parallel()

	T.Run("loads from file", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, SaveConfig(path, &Config{Email: "file@b.com", Password: "filepass"}))

		got, err := ResolveCredentials(path)
		require.NoError(t, err)
		assert.Equal(t, "file@b.com", got.Email)
		assert.Equal(t, "filepass", got.Password)
	})
}

// TestResolveCredentialsEnv covers the environment-override paths. These subtests use t.Setenv,
// which is incompatible with t.Parallel() (the runtime walks up to the parent), so this whole
// test tree runs serially — the one forced exception to the parallel-everywhere convention.
func TestResolveCredentialsEnv(T *testing.T) {
	T.Run("env overrides file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, SaveConfig(path, &Config{Email: "file@b.com", Password: "filepass"}))
		t.Setenv(EnvEmail, "env@b.com")
		t.Setenv(EnvPassword, "envpass")

		got, err := ResolveCredentials(path)
		require.NoError(t, err)
		assert.Equal(t, "env@b.com", got.Email)
		assert.Equal(t, "envpass", got.Password)
	})

	T.Run("env-only with no file", func(t *testing.T) {
		t.Setenv(EnvEmail, "env@b.com")
		t.Setenv(EnvPassword, "envpass")

		got, err := ResolveCredentials(filepath.Join(t.TempDir(), "absent.json"))
		require.NoError(t, err)
		assert.Equal(t, "env@b.com", got.Email)
	})

	T.Run("missing everything errors", func(t *testing.T) {
		t.Setenv(EnvEmail, "")
		t.Setenv(EnvPassword, "")

		_, err := ResolveCredentials(filepath.Join(t.TempDir(), "absent.json"))
		require.Error(t, err)
	})

	T.Run("missing password errors", func(t *testing.T) {
		t.Setenv(EnvEmail, "only@b.com")
		t.Setenv(EnvPassword, "")

		_, err := ResolveCredentials(filepath.Join(t.TempDir(), "absent.json"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "password")
	})
}
