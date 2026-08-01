package archive

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/verygoodsoftwarenotvirus/cronometer"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixtures mirror Cronometer's per-kind exports; the headers must match the candidate column names
// the parsers look up. The daily summary deliberately omits most nutrients so the archive's
// "unreported nutrient is NULL, not zero" behavior is exercised.
const (
	sampleDaily = `Date,Energy (kcal),Protein (g),Carbs (g),Fat (g)
2025-06-27,2100,150,200.5,70
2025-06-28,1950,140,190,0
`

	sampleServings = `Day,Time,Group,Food Name,Amount,Category
2025-06-27,08:15 ,"Breakfast","Oatmeal","1.00 cup","Cereal"
2025-06-27,19:30 ,"Dinner","Chicken Breast","150 g","Meats"
2025-06-28,12:00 ,"Lunch","Salad","1 bowl","Vegetables"
`

	sampleExercises = `Day,Time,Group,Exercise,Minutes,Calories Burned
2025-06-27,07:00 ,Data,Running,30,300
`

	sampleBiometrics = `Day,Time,Group,Metric,Unit,Amount
2025-06-27,06:00 ,Data,Weight (Apple Health),lbs,180
2025-06-28,06:00 ,Data,Weight (Apple Health),lbs,179.5
`

	sampleNotes = `Day,Time,Group,Note
2025-06-27,21:00 ,Data,"Felt great today"
`
)

// testData builds a Data by running the fixtures through the real parsers, so the tests cover the
// same path cmdArchive takes.
func testData(t *testing.T) *Data {
	t.Helper()

	days, err := cronometer.ParseDailyNutrition(sampleDaily)
	require.NoError(t, err)
	servings, err := cronometer.ParseServings(sampleServings)
	require.NoError(t, err)
	exercises, err := cronometer.ParseExercises(sampleExercises)
	require.NoError(t, err)
	biometrics, err := cronometer.ParseBiometrics(sampleBiometrics)
	require.NoError(t, err)
	notes, err := cronometer.ParseNotes(sampleNotes)
	require.NoError(t, err)

	return &Data{
		Year:       2025,
		Start:      time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		End:        time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC),
		Days:       days,
		Servings:   servings,
		Exercises:  exercises,
		Biometrics: biometrics,
		Notes:      notes,
		Raw: []RawExport{
			{Kind: "dailySummary", CSV: sampleDaily, Rows: len(days)},
			{Kind: "servings", CSV: sampleServings, Rows: len(servings)},
			{Kind: "exercises", CSV: sampleExercises, Rows: len(exercises)},
			{Kind: "biometrics", CSV: sampleBiometrics, Rows: len(biometrics)},
			{Kind: "notes", CSV: sampleNotes, Rows: len(notes)},
		},
	}
}

// openArchive opens a written archive read-only for assertions.
func openArchive(t *testing.T, path string) *sql.DB {
	t.Helper()

	db, err := sql.Open(driverName, path)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })

	return db
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()

	var n int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&n))

	return n
}

func TestWrite(T *testing.T) {
	T.Parallel()

	T.Run("row counts match the reported stats", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "2025.db")
		stats, err := Write(t.Context(), path, false, testData(t))
		require.NoError(t, err)

		assert.Equal(t, Stats{Days: 2, Servings: 3, Exercises: 1, Biometrics: 2, Notes: 1}, stats)

		db := openArchive(t, path)
		assert.Equal(t, 2, countRows(t, db, "daily_nutrition"))
		assert.Equal(t, 3, countRows(t, db, "serving"))
		assert.Equal(t, 1, countRows(t, db, "exercise"))
		assert.Equal(t, 2, countRows(t, db, "biometric"))
		assert.Equal(t, 1, countRows(t, db, "note"))
		assert.Equal(t, 5, countRows(t, db, "raw_export"))
		assert.Equal(t, len(cronometer.Registry), countRows(t, db, "nutrient"))
	})

	T.Run("reported nutrients round-trip and unreported ones are NULL", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "2025.db")
		_, err := Write(t.Context(), path, false, testData(t))
		require.NoError(t, err)

		db := openArchive(t, path)

		var energy, carbs, fat float64
		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT energy_kcal, carbs_g, fat_g FROM daily_nutrition WHERE date = '2025-06-27'`,
		).Scan(&energy, &carbs, &fat))
		assert.InDelta(t, 2100.0, energy, 0.001)
		assert.InDelta(t, 200.5, carbs, 0.001)
		assert.InDelta(t, 70.0, fat, 0.001)

		// Fiber was not a column in the export, so it is unknown — distinct from the genuine zero
		// recorded for fat on the 28th.
		var fiber, zeroFat sql.NullFloat64
		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT fiber_g, fat_g FROM daily_nutrition WHERE date = '2025-06-28'`,
		).Scan(&fiber, &zeroFat))
		assert.False(t, fiber.Valid, "fiber should be NULL when the export omits the column")
		require.True(t, zeroFat.Valid)
		assert.InDelta(t, 0.0, zeroFat.Float64, 0.001)
	})

	T.Run("biometrics record the source-stripped base metric", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "2025.db")
		_, err := Write(t.Context(), path, false, testData(t))
		require.NoError(t, err)

		db := openArchive(t, path)

		var metric, base, measuredAt string
		var value float64
		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT metric, base_metric, measured_at, value FROM biometric
			 WHERE base_metric = 'Weight' ORDER BY date LIMIT 1`,
		).Scan(&metric, &base, &measuredAt, &value))

		assert.Equal(t, "Weight (Apple Health)", metric)
		assert.Equal(t, "Weight", base)
		assert.Equal(t, "2025-06-27 06:00:00", measuredAt)
		assert.InDelta(t, 180.0, value, 0.001)
	})

	T.Run("metadata describes the run", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "2025.db")
		_, err := Write(t.Context(), path, false, testData(t))
		require.NoError(t, err)

		db := openArchive(t, path)

		rows, err := db.QueryContext(t.Context(), `SELECT key, value FROM archive_meta`)
		require.NoError(t, err)
		defer func() { assert.NoError(t, rows.Close()) }()

		meta := map[string]string{}
		for rows.Next() {
			var k, v string
			require.NoError(t, rows.Scan(&k, &v))
			meta[k] = v
		}
		require.NoError(t, rows.Err())

		assert.Equal(t, "1", meta["schema_version"])
		assert.Equal(t, "2025", meta["year"])
		assert.Equal(t, "2025-01-01", meta["range_start"])
		assert.Equal(t, "2025-12-31", meta["range_end"])

		generated, err := time.Parse(time.RFC3339, meta["generated_at"])
		require.NoError(t, err)
		assert.WithinDuration(t, time.Now(), generated, time.Minute)
	})

	T.Run("raw exports round-trip byte for byte", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "2025.db")
		_, err := Write(t.Context(), path, false, testData(t))
		require.NoError(t, err)

		db := openArchive(t, path)

		for kind, want := range map[string]string{
			"dailySummary": sampleDaily,
			"servings":     sampleServings,
			"exercises":    sampleExercises,
			"biometrics":   sampleBiometrics,
			"notes":        sampleNotes,
		} {
			var got string
			var byteCount int
			require.NoError(t, db.QueryRowContext(t.Context(),
				`SELECT csv, byte_count FROM raw_export WHERE kind = ?`, kind,
			).Scan(&got, &byteCount))

			assert.Equal(t, want, got, "raw export %q", kind)
			assert.Equal(t, len(want), byteCount, "raw export %q", kind)
		}
	})

	T.Run("nutrient table documents the generated columns", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "2025.db")
		_, err := Write(t.Context(), path, false, testData(t))
		require.NoError(t, err)

		db := openArchive(t, path)

		var column, csvColumn, unit string
		var isDefault int
		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT column_name, csv_column, unit, is_default FROM nutrient WHERE key = 'energy'`,
		).Scan(&column, &csvColumn, &unit, &isDefault))

		assert.Equal(t, "energy_kcal", column)
		assert.Equal(t, "Energy (kcal)", csvColumn)
		assert.Equal(t, "kcal", unit)
		assert.Equal(t, 1, isDefault)
	})

	T.Run("a year with no data yields a valid empty archive", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "empty.db")
		stats, err := Write(t.Context(), path, false, &Data{Year: 2024})
		require.NoError(t, err)
		assert.Equal(t, Stats{}, stats)

		db := openArchive(t, path)
		assert.Equal(t, 0, countRows(t, db, "daily_nutrition"))
		assert.Equal(t, 0, countRows(t, db, "raw_export"))
		assert.Equal(t, len(cronometer.Registry), countRows(t, db, "nutrient"))

		var check string
		require.NoError(t, db.QueryRowContext(t.Context(), `PRAGMA integrity_check`).Scan(&check))
		assert.Equal(t, "ok", check)
	})

	T.Run("writes 0600 permissions and leaves no temp file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "2025.db")
		_, err := Write(t.Context(), path, false, testData(t))
		require.NoError(t, err)

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

		_, err = os.Stat(path + ".tmp")
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	T.Run("refuses to clobber an existing file", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "2025.db")
		require.NoError(t, os.WriteFile(path, []byte("not a database"), 0o600))

		_, err := Write(t.Context(), path, false, testData(t))
		require.Error(t, err)

		// The refusal must be total: the original bytes are still there.
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "not a database", string(content))
	})

	T.Run("force overwrites an existing file", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "2025.db")
		require.NoError(t, os.WriteFile(path, []byte("not a database"), 0o600))

		stats, err := Write(t.Context(), path, true, testData(t))
		require.NoError(t, err)
		assert.Equal(t, 2, stats.Days)

		db := openArchive(t, path)
		assert.Equal(t, 2, countRows(t, db, "daily_nutrition"))
	})

	T.Run("a failed write leaves nothing behind", func(t *testing.T) {
		t.Parallel()

		// The parent directory does not exist, so the database can never be created.
		path := filepath.Join(t.TempDir(), "absent", "2025.db")

		_, err := Write(t.Context(), path, false, testData(t))
		require.Error(t, err)

		_, statErr := os.Stat(path)
		assert.ErrorIs(t, statErr, os.ErrNotExist)
		_, statErr = os.Stat(path + ".tmp")
		assert.ErrorIs(t, statErr, os.ErrNotExist)
	})
}

func TestCheckOutput(T *testing.T) {
	T.Parallel()

	T.Run("absent path is fine", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, CheckOutput(filepath.Join(t.TempDir(), "2025.db"), false))
	})

	T.Run("existing path errors without force", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "2025.db")
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))

		require.Error(t, CheckOutput(path, false))
		require.NoError(t, CheckOutput(path, true))
	})

	T.Run("empty path errors", func(t *testing.T) {
		t.Parallel()
		require.Error(t, CheckOutput("", false))
	})
}
