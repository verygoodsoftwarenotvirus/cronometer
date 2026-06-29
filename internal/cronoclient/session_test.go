package cronoclient

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionRoundTrip(T *testing.T) {
	T.Parallel()

	T.Run("save then load", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "nested", "session.json")
		want := Session{Nonce: "abc123", UserID: "42"}

		require.NoError(t, SaveSession(path, want))

		got, err := LoadSession(path)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	T.Run("writes 0600 permissions", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "session.json")
		require.NoError(t, SaveSession(path, Session{Nonce: "n", UserID: "u"}))

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})

	T.Run("load missing file errors", func(t *testing.T) {
		t.Parallel()
		_, err := LoadSession(filepath.Join(t.TempDir(), "absent.json"))
		require.Error(t, err)
	})
}

func TestClientSessionAccessors(T *testing.T) {
	T.Parallel()

	T.Run("restore then read back", func(t *testing.T) {
		t.Parallel()
		c := NewClient(nil)
		assert.False(t, c.HasSession())

		c.RestoreSession(Session{Nonce: "nonce-x", UserID: "99"})
		assert.True(t, c.HasSession())
		assert.Equal(t, Session{Nonce: "nonce-x", UserID: "99"}, c.Session())
	})
}
