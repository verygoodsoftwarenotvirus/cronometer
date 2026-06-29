package cronometer

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDays() []DayTotals {
	return []DayTotals{
		{Date: time.Date(2025, 6, 27, 0, 0, 0, 0, time.UTC), Values: map[string]float64{"energy": 2000, "protein": 150}},
		{Date: time.Date(2025, 6, 28, 0, 0, 0, 0, time.UTC), Values: map[string]float64{"energy": 1800, "protein": 120}},
	}
}

func testSelection(t *testing.T) []Nutrient {
	t.Helper()
	ns, err := ResolveList("energy,protein")
	require.NoError(t, err)
	return ns
}

func TestRenderTable(T *testing.T) {
	T.Parallel()

	T.Run("multi-day grid", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, FormatTable, testDays(), testSelection(t), false))
		out := buf.String()
		assert.Contains(t, out, "Date")
		assert.Contains(t, out, "energy (kcal)")
		assert.Contains(t, out, "2025-06-27")
		assert.Contains(t, out, "2000.0")
	})

	T.Run("single day is vertical", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, FormatTable, testDays()[:1], testSelection(t), false))
		out := buf.String()
		assert.Contains(t, out, "2025-06-27")
		assert.Contains(t, out, "energy")
		assert.Contains(t, out, "2000.0 kcal")
	})

	T.Run("averages row", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, FormatTable, testDays(), testSelection(t), true))
		out := buf.String()
		assert.Contains(t, out, "Average")
		assert.Contains(t, out, "1900.0")
	})

	T.Run("empty range", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, FormatTable, nil, testSelection(t), false))
		assert.Contains(t, buf.String(), "No data")
	})
}

func TestRenderJSON(T *testing.T) {
	T.Parallel()

	T.Run("emits array of day objects", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, FormatJSON, testDays(), testSelection(t), false))

		var got []struct {
			Nutrients map[string]struct {
				Unit  string  `json:"unit"`
				Value float64 `json:"value"`
			} `json:"nutrients"`
			Date string `json:"date"`
		}
		require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
		require.Len(t, got, 2)
		assert.Equal(t, "2025-06-27", got[0].Date)
		assert.InDelta(t, 2000.0, got[0].Nutrients["energy"].Value, 0.001)
		assert.Equal(t, "kcal", got[0].Nutrients["energy"].Unit)
	})
}

func TestRenderCSV(T *testing.T) {
	T.Parallel()

	T.Run("header and rows", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, FormatCSV, testDays(), testSelection(t), false))

		records, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
		require.NoError(t, err)
		require.Len(t, records, 3) // header + 2 days
		assert.Equal(t, []string{"Date", "Energy (kcal)", "Protein (g)"}, records[0])
		assert.Equal(t, "2025-06-27", records[1][0])
		assert.Equal(t, "2000", records[1][1])
	})

	T.Run("averages row appended", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, FormatCSV, testDays(), testSelection(t), true))

		records, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
		require.NoError(t, err)
		require.Len(t, records, 4)
		assert.Equal(t, "Average", records[3][0])
		assert.Equal(t, "1900", records[3][1])
	})
}

func TestParseFormat(T *testing.T) {
	T.Parallel()

	T.Run("valid formats", func(t *testing.T) {
		t.Parallel()
		for _, s := range []string{"table", "json", "csv"} {
			f, err := ParseFormat(s)
			require.NoErrorf(t, err, "for %q", s)
			assert.Equal(t, Format(s), f)
		}
	})

	T.Run("invalid format", func(t *testing.T) {
		t.Parallel()
		_, err := ParseFormat("xml")
		require.Error(t, err)
	})
}
