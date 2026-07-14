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

// testDaysWithWeight returns two days where the first carries a weigh-in and the second does not,
// exercising both the present and blank weight-cell paths.
func testDaysWithWeight() []DayTotals {
	days := testDays()
	days[0].Weight = &Measurement{Value: 180, Unit: "lbs"}
	return days
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

func TestRenderWeight(T *testing.T) {
	T.Parallel()

	T.Run("table multi-day shows a weight column, blank when missing", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, FormatTable, testDaysWithWeight(), testSelection(t), false))
		out := buf.String()
		assert.Contains(t, out, "weight (lbs)")
		assert.Contains(t, out, "180.0")
	})

	T.Run("table single-day appends a weight line", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, FormatTable, testDaysWithWeight()[:1], testSelection(t), false))
		assert.Contains(t, buf.String(), "weight")
		assert.Contains(t, buf.String(), "180.0 lbs")
	})

	T.Run("table averages the present weigh-ins", func(t *testing.T) {
		t.Parallel()
		days := testDaysWithWeight()
		days[1].Weight = &Measurement{Value: 178, Unit: "lbs"}
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, FormatTable, days, testSelection(t), true))
		assert.Contains(t, buf.String(), "179.0") // (180 + 178) / 2
	})

	T.Run("no weight on any day omits the column", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, FormatTable, testDays(), testSelection(t), false))
		assert.NotContains(t, buf.String(), "weight")
	})

	T.Run("csv adds a weight column, blank when missing", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, FormatCSV, testDaysWithWeight(), testSelection(t), false))
		records, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
		require.NoError(t, err)
		assert.Equal(t, "Weight (lbs)", records[0][len(records[0])-1])
		assert.Equal(t, "180", records[1][len(records[1])-1])
		assert.Equal(t, "", records[2][len(records[2])-1]) // day 2 has no weigh-in
	})

	T.Run("json nests weight beside nutrients, omitted when absent", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, FormatJSON, testDaysWithWeight(), testSelection(t), false))

		var got []struct {
			Weight *struct {
				Unit  string  `json:"unit"`
				Value float64 `json:"value"`
			} `json:"weight"`
			Date string `json:"date"`
		}
		require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
		require.Len(t, got, 2)
		require.NotNil(t, got[0].Weight)
		assert.InDelta(t, 180.0, got[0].Weight.Value, 0.001)
		assert.Equal(t, "lbs", got[0].Weight.Unit)
		assert.Nil(t, got[1].Weight)
	})

	T.Run("weight and body fat render as separate columns", func(t *testing.T) {
		t.Parallel()
		days := testDays()
		days[0].Weight = &Measurement{Value: 180, Unit: "lbs"}
		days[0].BodyFat = &Measurement{Value: 18, Unit: "%"}
		days[1].BodyFat = &Measurement{Value: 17.5, Unit: "%"}

		var tbl bytes.Buffer
		require.NoError(t, Render(&tbl, FormatTable, days, testSelection(t), false))
		assert.Contains(t, tbl.String(), "weight (lbs)")
		assert.Contains(t, tbl.String(), "body fat (%)")

		var cbuf bytes.Buffer
		require.NoError(t, Render(&cbuf, FormatCSV, days, testSelection(t), false))
		records, err := csv.NewReader(strings.NewReader(cbuf.String())).ReadAll()
		require.NoError(t, err)
		header := records[0]
		assert.Equal(t, "Weight (lbs)", header[len(header)-2])
		assert.Equal(t, "Body Fat (%)", header[len(header)-1])
		// Day 2 has body fat but no weigh-in: weight cell blank, body-fat cell filled.
		assert.Equal(t, "", records[2][len(records[2])-2])
		assert.Equal(t, "17.5", records[2][len(records[2])-1])

		var jbuf bytes.Buffer
		require.NoError(t, Render(&jbuf, FormatJSON, days, testSelection(t), false))
		var got []struct {
			BodyFat *struct {
				Unit  string  `json:"unit"`
				Value float64 `json:"value"`
			} `json:"bodyFat"`
		}
		require.NoError(t, json.Unmarshal(jbuf.Bytes(), &got))
		require.Len(t, got, 2)
		require.NotNil(t, got[0].BodyFat)
		assert.InDelta(t, 18.0, got[0].BodyFat.Value, 0.001)
		assert.Equal(t, "%", got[0].BodyFat.Unit)
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
