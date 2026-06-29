package cronometer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rfcSecret is the RFC 6238 Appendix B test seed "12345678901234567890" in base32.
const rfcSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestGenerateTOTP(T *testing.T) {
	T.Parallel()

	T.Run("matches RFC 6238 SHA-1 vectors (6-digit)", func(t *testing.T) {
		t.Parallel()
		// The RFC publishes 8-digit codes; these are their last 6 digits.
		cases := map[int64]string{
			59:         "287082",
			1111111109: "081804",
			1111111111: "050471",
			1234567890: "005924",
			2000000000: "279037",
		}
		for unix, want := range cases {
			got, err := GenerateTOTP(rfcSecret, time.Unix(unix, 0))
			require.NoErrorf(t, err, "at %d", unix)
			assert.Equalf(t, want, got, "at unix %d", unix)
		}
	})

	T.Run("ignores spaces and case in the secret", func(t *testing.T) {
		t.Parallel()
		spaced := "gezd gnbv gy3t qojq gezd gnbv gy3t qojq"
		got, err := GenerateTOTP(spaced, time.Unix(59, 0))
		require.NoError(t, err)
		assert.Equal(t, "287082", got)
	})

	T.Run("stable within a 30s step, changes across it", func(t *testing.T) {
		t.Parallel()
		a, err := GenerateTOTP(rfcSecret, time.Unix(60, 0))
		require.NoError(t, err)
		b, err := GenerateTOTP(rfcSecret, time.Unix(89, 0))
		require.NoError(t, err)
		c, err := GenerateTOTP(rfcSecret, time.Unix(90, 0))
		require.NoError(t, err)
		assert.Equal(t, a, b, "same 30s window should match")
		assert.NotEqual(t, a, c, "next window should differ")
	})

	T.Run("rejects an invalid secret", func(t *testing.T) {
		t.Parallel()
		_, err := GenerateTOTP("not!base32!", time.Unix(59, 0))
		require.Error(t, err)
	})

	T.Run("rejects an empty secret", func(t *testing.T) {
		t.Parallel()
		_, err := GenerateTOTP("", time.Unix(59, 0))
		require.Error(t, err)
	})
}

func TestConfigTOTPCode(T *testing.T) {
	T.Parallel()

	T.Run("generates when secret is set", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{TOTPSecret: rfcSecret}
		code, ok, err := cfg.TOTPCode(time.Unix(59, 0))
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, "287082", code)
	})

	T.Run("signals prompt-needed when no secret", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{}
		code, ok, err := cfg.TOTPCode(time.Unix(59, 0))
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Empty(t, code)
	})
}
