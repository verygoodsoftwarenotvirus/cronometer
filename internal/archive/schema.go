// Package archive writes a year of Cronometer data into a self-describing SQLite database whose
// schema this application owns. The nutrient columns are generated from cronometer.Registry rather
// than hand-written, so the two cannot drift.
package archive

import (
	"fmt"
	"strings"

	"github.com/verygoodsoftwarenotvirus/cronometer"
)

// Version is the archive schema version, recorded in archive_meta. Bump it whenever the emitted
// DDL changes shape.
const Version = 1

// unitSlug maps a registry nutrient's display unit to the suffix used in its SQLite column name.
// Every unit appearing in cronometer.Registry must be listed here: an unmapped unit is an error
// rather than a guess, so adding a nutrient with a novel unit fails the schema test instead of
// silently producing a malformed column.
var unitSlug = map[string]string{
	"kcal": "kcal",
	"g":    "g",
	"mg":   "mg",
	"µg":   "ug",
	"IU":   "iu",
}

// Column returns the daily_nutrition column name for a nutrient: its key with hyphens converted to
// underscores, suffixed with the unit slug ("net-carbs"/g becomes "net_carbs_g", "vitamin-a"/µg
// becomes "vitamin_a_ug").
func Column(n *cronometer.Nutrient) (string, error) {
	slug, ok := unitSlug[n.Unit]
	if !ok {
		return "", fmt.Errorf("nutrient %q has unmapped unit %q; add it to unitSlug", n.Key, n.Unit)
	}
	return strings.ReplaceAll(n.Key, "-", "_") + "_" + slug, nil
}

// Columns returns the daily_nutrition nutrient columns in registry order, positionally parallel to
// cronometer.Registry.
func Columns() ([]string, error) {
	cols := make([]string, 0, len(cronometer.Registry))
	for i := range cronometer.Registry {
		col, err := Column(&cronometer.Registry[i])
		if err != nil {
			return nil, err
		}
		cols = append(cols, col)
	}
	return cols, nil
}

// headDDL is everything preceding the generated daily_nutrition table. The tables are STRICT so
// SQLite enforces the column types rather than silently coercing them.
const headDDL = `
CREATE TABLE archive_meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
) STRICT;

CREATE TABLE nutrient (
	key           TEXT PRIMARY KEY,
	column_name   TEXT NOT NULL UNIQUE,
	csv_column    TEXT NOT NULL,
	unit          TEXT NOT NULL,
	display_order INTEGER NOT NULL,
	is_default    INTEGER NOT NULL
) STRICT;
`

// tailDDL is everything following the generated daily_nutrition table. Dates are stored as
// YYYY-MM-DD text in every table, so they sort and compare lexicographically.
const tailDDL = `
CREATE TABLE serving (
	id       INTEGER PRIMARY KEY,
	date     TEXT NOT NULL,
	time     TEXT,
	meal     TEXT,
	food     TEXT,
	amount   TEXT,
	category TEXT
) STRICT;

CREATE INDEX serving_date ON serving(date);

CREATE TABLE exercise (
	id       INTEGER PRIMARY KEY,
	date     TEXT NOT NULL,
	name     TEXT,
	minutes  REAL,
	calories REAL
) STRICT;

CREATE INDEX exercise_date ON exercise(date);

CREATE TABLE biometric (
	id          INTEGER PRIMARY KEY,
	date        TEXT NOT NULL,
	measured_at TEXT,
	metric      TEXT,
	base_metric TEXT,
	unit        TEXT,
	value       REAL
) STRICT;

CREATE INDEX biometric_date ON biometric(date);
CREATE INDEX biometric_base_metric ON biometric(base_metric);

CREATE TABLE note (
	id   INTEGER PRIMARY KEY,
	date TEXT NOT NULL,
	text TEXT NOT NULL
) STRICT;

CREATE INDEX note_date ON note(date);

CREATE TABLE raw_export (
	kind       TEXT PRIMARY KEY,
	csv        TEXT NOT NULL,
	byte_count INTEGER NOT NULL,
	row_count  INTEGER NOT NULL
) STRICT;
`

// DDL returns the complete schema script: the fixed tables plus the daily_nutrition table generated
// from cronometer.Registry.
func DDL() (string, error) {
	cols, err := Columns()
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(headDDL)
	b.WriteString("\nCREATE TABLE daily_nutrition (\n\tdate TEXT PRIMARY KEY")
	for _, col := range cols {
		fmt.Fprintf(&b, ",\n\t%s REAL", col)
	}
	b.WriteString("\n) STRICT;\n")
	b.WriteString(tailDDL)

	return b.String(), nil
}
