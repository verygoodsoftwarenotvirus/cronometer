package cronometer

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixtures mirror Cronometer's per-kind exports. The header names here must match the candidate
// column names the parsers look up (see detail.go); reconcile against a real export if a section
// comes back empty.
const (
	sampleServings = `Day,Time,Group,Food Name,Amount,Category
2025-06-27,08:15 ,"Breakfast","Oatmeal","1.00 cup","Cereal"
2025-06-27,08:20 ,"Breakfast","Banana","1 medium","Fruits"
2025-06-27,19:30 ,"Dinner","Chicken Breast","150 g","Meats"
2025-06-28,12:00 ,"Lunch","Salad","1 bowl","Vegetables"
`

	sampleExercises = `Day,Time,Group,Exercise,Minutes,Calories Burned
2025-06-27,07:00 ,Data,Running,30,300
2025-06-27,18:00 ,Data,Yoga,45,120
`

	// Weight is a single daily reading; Heart Rate is sampled repeatedly and must aggregate.
	sampleBiometrics = `Day,Time,Group,Metric,Unit,Amount
2025-06-27,06:00 ,Data,Weight,lbs,180
2025-06-27,06:01 ,Data,Heart Rate,bpm,60
2025-06-27,06:02 ,Data,Heart Rate,bpm,80
2025-06-27,06:03 ,Data,Heart Rate,bpm,70
2025-06-28,06:00 ,Data,Weight,lbs,179.5
`

	sampleNotes = `Day,Time,Group,Note
2025-06-27,21:00 ,Data,"Felt great today"
2025-06-28,,Data,
`
)

func TestParseServings(T *testing.T) {
	T.Parallel()

	T.Run("parses foods with meal grouping and macros", func(t *testing.T) {
		t.Parallel()
		entries, err := ParseServings(sampleServings)
		require.NoError(t, err)
		require.Len(t, entries, 4)

		first := entries[0]
		assert.Equal(t, time.Date(2025, 6, 27, 0, 0, 0, 0, time.UTC), first.Date)
		assert.Equal(t, "08:15", first.Time)
		assert.Equal(t, "Breakfast", first.Meal)
		assert.Equal(t, "Oatmeal", first.Food)
		assert.Equal(t, "1.00 cup", first.Amount)
		assert.Equal(t, "Cereal", first.Category)
	})

	T.Run("empty export yields no entries and no error", func(t *testing.T) {
		t.Parallel()
		entries, err := ParseServings("")
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	T.Run("missing meal group defaults to Uncategorized", func(t *testing.T) {
		t.Parallel()
		entries, err := ParseServings("Day,Food Name,Amount,Energy (kcal)\n2025-06-27,Water,500 ml,0\n")
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, "Uncategorized", entries[0].Meal)
	})
}

func TestParseExercises(t *testing.T) {
	t.Parallel()

	entries, err := ParseExercises(sampleExercises)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "Running", entries[0].Name)
	assert.InDelta(t, 30.0, entries[0].Minutes, 0.001)
	assert.InDelta(t, 300.0, entries[0].Calories, 0.001)
}

func TestParseBiometrics(t *testing.T) {
	t.Parallel()

	// ParseBiometrics returns raw per-reading rows; aggregation happens in BuildDayDetails.
	entries, err := ParseBiometrics(sampleBiometrics)
	require.NoError(t, err)
	require.Len(t, entries, 5)
	assert.Equal(t, "Weight", entries[0].Metric)
	assert.Equal(t, "lbs", entries[0].Unit)
	assert.InDelta(t, 180.0, entries[0].Value, 0.001)
	assert.Equal(t, "Heart Rate", entries[1].Metric)
}

func TestParseNotes(t *testing.T) {
	t.Parallel()

	entries, err := ParseNotes(sampleNotes)
	require.NoError(t, err)
	// The blank note on 2025-06-28 is skipped.
	require.Len(t, entries, 1)
	assert.Equal(t, "Felt great today", entries[0].Text)
}

func TestBuildDayDetails(T *testing.T) {
	T.Parallel()

	build := func(t *testing.T) []DayDetail {
		t.Helper()
		days, err := ParseDailyNutrition(sampleExport)
		require.NoError(t, err)
		servings, err := ParseServings(sampleServings)
		require.NoError(t, err)
		exercises, err := ParseExercises(sampleExercises)
		require.NoError(t, err)
		biometrics, err := ParseBiometrics(sampleBiometrics)
		require.NoError(t, err)
		notes, err := ParseNotes(sampleNotes)
		require.NoError(t, err)
		return BuildDayDetails(days, servings, exercises, biometrics, notes)
	}

	T.Run("joins sources by date, ascending", func(t *testing.T) {
		t.Parallel()
		details := build(t)
		require.Len(t, details, 2)
		assert.Equal(t, time.Date(2025, 6, 27, 0, 0, 0, 0, time.UTC), details[0].Date)
		assert.Equal(t, time.Date(2025, 6, 28, 0, 0, 0, 0, time.UTC), details[1].Date)
	})

	T.Run("orders meals canonically and lists foods", func(t *testing.T) {
		t.Parallel()
		day := build(t)[0]
		require.Len(t, day.Meals, 2)
		// Breakfast precedes Dinner even though a Dinner row isn't last in the CSV order group.
		assert.Equal(t, "Breakfast", day.Meals[0].Name)
		assert.Equal(t, "Dinner", day.Meals[1].Name)

		breakfast := day.Meals[0]
		require.Len(t, breakfast.Foods, 2)
		assert.Equal(t, "Oatmeal", breakfast.Foods[0].Food)
		assert.Equal(t, "Banana", breakfast.Foods[1].Food)
	})

	T.Run("aggregates repeated biometrics into min/avg/max", func(t *testing.T) {
		t.Parallel()
		day := build(t)[0]
		// Weight (1 reading) + Heart Rate (3 readings) => 2 summaries.
		require.Len(t, day.Biometrics, 2)

		weight := day.Biometrics[0]
		assert.Equal(t, "Weight", weight.Metric)
		assert.Equal(t, 1, weight.Count)
		assert.InDelta(t, 180.0, weight.Last, 0.001)

		hr := day.Biometrics[1]
		assert.Equal(t, "Heart Rate", hr.Metric)
		assert.Equal(t, 3, hr.Count)
		assert.InDelta(t, 60.0, hr.Min, 0.001)
		assert.InDelta(t, 80.0, hr.Max, 0.001)
		assert.InDelta(t, 70.0, hr.Avg, 0.001)
	})

	T.Run("attaches activity, notes, and totals", func(t *testing.T) {
		t.Parallel()
		day := build(t)[0]
		require.Len(t, day.Exercises, 2)
		require.Len(t, day.Notes, 1)
		// Totals come from the daily-summary export (sampleExport).
		assert.InDelta(t, 2000.0, day.Totals["energy"], 0.001)
	})
}

func TestRenderDetailed(T *testing.T) {
	T.Parallel()

	buildDetails := func(t *testing.T) []DayDetail {
		t.Helper()
		days, err := ParseDailyNutrition(sampleExport)
		require.NoError(t, err)
		servings, err := ParseServings(sampleServings)
		require.NoError(t, err)
		exercises, err := ParseExercises(sampleExercises)
		require.NoError(t, err)
		biometrics, err := ParseBiometrics(sampleBiometrics)
		require.NoError(t, err)
		notes, err := ParseNotes(sampleNotes)
		require.NoError(t, err)
		return BuildDayDetails(days, servings, exercises, biometrics, notes)
	}

	T.Run("table shows meals, foods, and sections", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		require.NoError(t, RenderDetailed(&buf, FormatTable, buildDetails(t), testSelection(t)))
		out := buf.String()
		assert.Contains(t, out, "2025-06-27")
		assert.Contains(t, out, "Breakfast")
		assert.Contains(t, out, "Oatmeal")
		assert.Contains(t, out, "1.00 cup")
		assert.Contains(t, out, "Activity")
		assert.Contains(t, out, "Running")
		assert.Contains(t, out, "Biomarkers")
		assert.Contains(t, out, "Weight")
		// Heart Rate has multiple samples => aggregated line.
		assert.Contains(t, out, "samples")
		assert.Contains(t, out, "Notes")
		assert.Contains(t, out, "Felt great today")
		assert.Contains(t, out, "Daily total")
	})

	T.Run("json is well-formed and nested", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		require.NoError(t, RenderDetailed(&buf, FormatJSON, buildDetails(t), testSelection(t)))

		var got []map[string]any
		require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
		require.Len(t, got, 2)
		assert.Equal(t, "2025-06-27", got[0]["date"])

		meals, ok := got[0]["meals"].([]any)
		require.True(t, ok)
		require.NotEmpty(t, meals)
		first, ok := meals[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "Breakfast", first["name"])
	})

	T.Run("csv is rejected", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		err := RenderDetailed(&buf, FormatCSV, buildDetails(t), testSelection(t))
		require.Error(t, err)
	})

	T.Run("empty details render a friendly message", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		require.NoError(t, RenderDetailed(&buf, FormatTable, nil, testSelection(t)))
		assert.Contains(t, buf.String(), "No data")
	})
}
