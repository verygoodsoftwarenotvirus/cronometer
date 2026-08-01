package archive

import (
	"regexp"
	"strings"
	"testing"

	"github.com/verygoodsoftwarenotvirus/cronometer"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bareIdentifier matches a SQLite column name that needs no quoting.
var bareIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func TestColumns(T *testing.T) {
	T.Parallel()

	// This is the guard that makes the generated schema safe: if Cronometer adds a nutrient with a
	// unit no one has mapped yet, the failure surfaces here rather than as a malformed column.
	T.Run("every registry nutrient yields a column", func(t *testing.T) {
		t.Parallel()

		cols, err := Columns()
		require.NoError(t, err)
		require.Len(t, cols, len(cronometer.Registry))
	})

	T.Run("column names are unique bare identifiers", func(t *testing.T) {
		t.Parallel()

		cols, err := Columns()
		require.NoError(t, err)

		seen := make(map[string]struct{}, len(cols))
		for _, col := range cols {
			assert.Regexp(t, bareIdentifier, col)
			_, dup := seen[col]
			assert.False(t, dup, "duplicate column %q", col)
			seen[col] = struct{}{}
		}
	})

	T.Run("known nutrients map to expected columns", func(t *testing.T) {
		t.Parallel()

		for name, want := range map[string]string{
			"energy":      "energy_kcal",
			"protein":     "protein_g",
			"net-carbs":   "net_carbs_g",
			"vitamin-a":   "vitamin_a_ug",
			"vitamin-d":   "vitamin_d_iu",
			"cholesterol": "cholesterol_mg",
		} {
			n, ok := cronometer.ResolveNutrient(name)
			require.True(t, ok, "nutrient %q missing from registry", name)

			got, err := Column(&n)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		}
	})

	T.Run("unmapped unit errors", func(t *testing.T) {
		t.Parallel()

		_, err := Column(&cronometer.Nutrient{Key: "unobtainium", Unit: "furlongs"})
		require.Error(t, err)
	})
}

func TestDDL(T *testing.T) {
	T.Parallel()

	T.Run("declares a column for every nutrient in registry order", func(t *testing.T) {
		t.Parallel()

		ddl, err := DDL()
		require.NoError(t, err)

		cols, err := Columns()
		require.NoError(t, err)

		prev := strings.Index(ddl, "CREATE TABLE daily_nutrition")
		require.Positive(t, prev)
		for _, col := range cols {
			at := strings.Index(ddl, "\n\t"+col+" REAL")
			require.Positive(t, at, "no declaration for column %q", col)
			assert.Greater(t, at, prev, "column %q out of registry order", col)
			prev = at
		}
	})

	T.Run("creates every expected table", func(t *testing.T) {
		t.Parallel()

		ddl, err := DDL()
		require.NoError(t, err)

		for _, table := range []string{
			"archive_meta", "nutrient", "daily_nutrition",
			"serving", "exercise", "biometric", "note", "raw_export",
		} {
			assert.Contains(t, ddl, "CREATE TABLE "+table+" (")
		}
	})
}
