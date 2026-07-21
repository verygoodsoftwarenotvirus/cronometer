package cronometer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleExport mimics Cronometer's daily-summary CSV: a Date column, a handful of nutrient
// columns, plus a "Completed" column the parser should ignore.
const sampleExport = `Date,Energy (kcal),Protein (g),Fiber (g),Caffeine (mg),Completed
2025-06-27,2000,150,30,95,true
2025-06-28,1800.5,120.2,25,,false
`

func TestParseDailyNutrition(T *testing.T) {
	T.Parallel()

	T.Run("parses multi-day export", func(t *testing.T) {
		t.Parallel()
		days, err := ParseDailyNutrition(sampleExport)
		require.NoError(t, err)
		require.Len(t, days, 2)

		assert.Equal(t, time.Date(2025, 6, 27, 0, 0, 0, 0, time.UTC), days[0].Date)
		assert.InDelta(t, 2000.0, days[0].Values["energy"], 0.001)
		assert.InDelta(t, 150.0, days[0].Values["protein"], 0.001)
		assert.InDelta(t, 30.0, days[0].Values["fiber"], 0.001)
		assert.InDelta(t, 95.0, days[0].Values["caffeine"], 0.001)

		// Empty cell parses as zero.
		assert.InDelta(t, 0.0, days[1].Values["caffeine"], 0.001)
		assert.InDelta(t, 1800.5, days[1].Values["energy"], 0.001)
	})

	T.Run("ignores unknown columns like Completed", func(t *testing.T) {
		t.Parallel()
		days, err := ParseDailyNutrition(sampleExport)
		require.NoError(t, err)
		_, ok := days[0].Values["completed"]
		assert.False(t, ok)
	})

	T.Run("missing nutrient columns are absent (zero value)", func(t *testing.T) {
		t.Parallel()
		days, err := ParseDailyNutrition(sampleExport)
		require.NoError(t, err)
		_, ok := days[0].Values["sodium"]
		assert.False(t, ok)
		assert.InDelta(t, 0.0, days[0].Values["sodium"], 0.001)
	})

	T.Run("accepts Day as the date column", func(t *testing.T) {
		t.Parallel()
		days, err := ParseDailyNutrition("Day,Protein (g)\n2025-01-01,42\n")
		require.NoError(t, err)
		require.Len(t, days, 1)
		assert.InDelta(t, 42.0, days[0].Values["protein"], 0.001)
	})

	T.Run("empty input yields no days", func(t *testing.T) {
		t.Parallel()
		days, err := ParseDailyNutrition("")
		require.NoError(t, err)
		assert.Empty(t, days)
	})

	T.Run("header only yields no days", func(t *testing.T) {
		t.Parallel()
		days, err := ParseDailyNutrition("Date,Protein (g)\n")
		require.NoError(t, err)
		assert.Empty(t, days)
	})

	T.Run("errors without a date column", func(t *testing.T) {
		t.Parallel()
		_, err := ParseDailyNutrition("Protein (g)\n42\n")
		require.Error(t, err)
	})

	T.Run("errors on bad date", func(t *testing.T) {
		t.Parallel()
		_, err := ParseDailyNutrition("Date,Protein (g)\nnotadate,42\n")
		require.Error(t, err)
	})

	T.Run("errors on non-numeric nutrient value", func(t *testing.T) {
		t.Parallel()
		_, err := ParseDailyNutrition("Date,Protein (g)\n2025-01-01,lots\n")
		require.Error(t, err)
	})
}

func TestAttachBodyMetrics(T *testing.T) {
	T.Parallel()

	newDays := func() []DayTotals {
		return []DayTotals{
			{Date: time.Date(2025, 6, 27, 0, 0, 0, 0, time.UTC), Values: map[string]float64{"energy": 2000}},
			{Date: time.Date(2025, 6, 28, 0, 0, 0, 0, time.UTC), Values: map[string]float64{"energy": 1800}},
		}
	}

	T.Run("keeps the first same-day weigh-in and ignores other metrics", func(t *testing.T) {
		t.Parallel()
		day := time.Date(2025, 6, 27, 0, 0, 0, 0, time.UTC)
		bio := []BiometricEntry{
			// Logged out of order: the 08:00 reading comes first in the slice, but 06:00 is the first of the day.
			{Date: day, Time: day.Add(8 * time.Hour), Metric: "Weight", Unit: "lbs", Value: 182},
			{Date: day, Time: day.Add(6 * time.Hour), Metric: "Weight", Unit: "lbs", Value: 180},
			{Date: day, Time: day.Add(6 * time.Hour), Metric: "Heart Rate", Unit: "bpm", Value: 60},
			{Date: time.Date(2025, 6, 28, 0, 0, 0, 0, time.UTC), Metric: "Weight", Unit: "lbs", Value: 179.5},
		}
		days := newDays()
		AttachBodyMetrics(days, bio)

		require.NotNil(t, days[0].Weight)
		assert.InDelta(t, 180.0, days[0].Weight.Value, 0.001)
		assert.Equal(t, "lbs", days[0].Weight.Unit)

		require.NotNil(t, days[1].Weight)
		assert.InDelta(t, 179.5, days[1].Weight.Value, 0.001)
	})

	T.Run("leaves days without a weigh-in nil", func(t *testing.T) {
		t.Parallel()
		bio := []BiometricEntry{
			{Date: time.Date(2025, 6, 27, 0, 0, 0, 0, time.UTC), Metric: "Weight", Unit: "kg", Value: 82},
		}
		days := newDays()
		AttachBodyMetrics(days, bio)
		require.NotNil(t, days[0].Weight)
		assert.Nil(t, days[1].Weight)
	})

	T.Run("matches the Weight metric case-insensitively", func(t *testing.T) {
		t.Parallel()
		bio := []BiometricEntry{
			{Date: time.Date(2025, 6, 27, 0, 0, 0, 0, time.UTC), Metric: "weight", Unit: "kg", Value: 82},
		}
		days := newDays()
		AttachBodyMetrics(days, bio)
		require.NotNil(t, days[0].Weight)
		assert.InDelta(t, 82.0, days[0].Weight.Value, 0.001)
	})

	T.Run("matches a source-suffixed metric and keeps weight and body fat separate", func(t *testing.T) {
		t.Parallel()
		// Real exports name the metrics "Weight (Apple Health)" / "Body Fat (Apple Health)"; the body-fat
		// reading must land in BodyFat, never averaged into Weight.
		bio := []BiometricEntry{
			{Date: time.Date(2025, 6, 27, 0, 0, 0, 0, time.UTC), Metric: "Weight (Apple Health)", Unit: "lbs", Value: 172.5},
			{Date: time.Date(2025, 6, 27, 0, 0, 0, 0, time.UTC), Metric: "Body Fat (Apple Health)", Unit: "%", Value: 18},
			{Date: time.Date(2025, 6, 28, 0, 0, 0, 0, time.UTC), Metric: "Weight (Withings)", Unit: "lbs", Value: 171},
		}
		days := newDays()
		AttachBodyMetrics(days, bio)

		require.NotNil(t, days[0].Weight)
		assert.InDelta(t, 172.5, days[0].Weight.Value, 0.001)
		assert.Equal(t, "lbs", days[0].Weight.Unit)

		require.NotNil(t, days[0].BodyFat)
		assert.InDelta(t, 18.0, days[0].BodyFat.Value, 0.001)
		assert.Equal(t, "%", days[0].BodyFat.Unit)

		require.NotNil(t, days[1].Weight)
		assert.InDelta(t, 171.0, days[1].Weight.Value, 0.001)
		assert.Nil(t, days[1].BodyFat)
	})

	T.Run("keeps the first same-day body-fat reading", func(t *testing.T) {
		t.Parallel()
		day := time.Date(2025, 6, 27, 0, 0, 0, 0, time.UTC)
		bio := []BiometricEntry{
			{Date: day, Time: day.Add(6 * time.Hour), Metric: "Body Fat (Apple Health)", Unit: "%", Value: 18},
			{Date: day, Time: day.Add(20 * time.Hour), Metric: "Body Fat (Apple Health)", Unit: "%", Value: 20},
		}
		days := newDays()
		AttachBodyMetrics(days, bio)
		require.NotNil(t, days[0].BodyFat)
		assert.InDelta(t, 18.0, days[0].BodyFat.Value, 0.001)
		assert.Nil(t, days[0].Weight)
	})

	T.Run("no biometrics leaves all days nil", func(t *testing.T) {
		t.Parallel()
		days := newDays()
		AttachBodyMetrics(days, nil)
		assert.Nil(t, days[0].Weight)
		assert.Nil(t, days[1].Weight)
		assert.Nil(t, days[0].BodyFat)
	})
}

func TestSelectNutrients(T *testing.T) {
	T.Parallel()

	T.Run("defaults to macros", func(t *testing.T) {
		t.Parallel()
		got, err := SelectNutrients(SelectionOptions{})
		require.NoError(t, err)
		assert.Equal(t, []string{"energy", "protein", "carbs", "fat"}, nutrientKeys(got))
	})

	T.Run("add extends the defaults", func(t *testing.T) {
		t.Parallel()
		got, err := SelectNutrients(SelectionOptions{Add: "fiber,caffeine"})
		require.NoError(t, err)
		assert.Equal(t, []string{"energy", "protein", "carbs", "fat", "fiber", "caffeine"}, nutrientKeys(got))
	})

	T.Run("only replaces the defaults", func(t *testing.T) {
		t.Parallel()
		got, err := SelectNutrients(SelectionOptions{Only: "protein"})
		require.NoError(t, err)
		assert.Equal(t, []string{"protein"}, nutrientKeys(got))
	})

	T.Run("exclude subtracts from the resulting set", func(t *testing.T) {
		t.Parallel()
		got, err := SelectNutrients(SelectionOptions{Exclude: "energy"})
		require.NoError(t, err)
		assert.Equal(t, []string{"protein", "carbs", "fat"}, nutrientKeys(got))
	})

	T.Run("all wins over only", func(t *testing.T) {
		t.Parallel()
		got, err := SelectNutrients(SelectionOptions{All: true, Only: "protein"})
		require.NoError(t, err)
		assert.Len(t, got, len(Registry))
	})

	T.Run("only combined with exclude", func(t *testing.T) {
		t.Parallel()
		got, err := SelectNutrients(SelectionOptions{Only: "protein,fiber,fat", Exclude: "fiber"})
		require.NoError(t, err)
		assert.Equal(t, []string{"protein", "fat"}, nutrientKeys(got))
	})

	T.Run("propagates unknown-nutrient errors", func(t *testing.T) {
		t.Parallel()
		_, err := SelectNutrients(SelectionOptions{Add: "bogus"})
		require.Error(t, err)
	})
}
