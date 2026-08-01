package archive

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/verygoodsoftwarenotvirus/cronometer"
	"github.com/verygoodsoftwarenotvirus/cronometer/version"

	// Pure-Go SQLite, registered with database/sql under the name "sqlite".
	_ "modernc.org/sqlite"
)

const (
	// driverName is the database/sql driver modernc.org/sqlite registers itself as.
	driverName = "sqlite"

	// archiveMode is the permission the finished archive gets. It holds a year of personal health
	// data, so it is owner-only like the config and session files.
	archiveMode = 0o600

	// timestampLayout is how biometric reading times are stored: SQLite's date functions understand
	// this form, and it sorts lexicographically.
	timestampLayout = "2006-01-02 15:04:05"
)

// RawExport is one export's unparsed CSV body, stored verbatim so the archive stays lossless even
// where the parsers ignore columns.
type RawExport struct {
	// Kind is Cronometer's export name ("dailySummary", "servings", "exercises", ...).
	Kind string
	// CSV is the response body exactly as Cronometer returned it.
	CSV string
	// Rows is how many records the CSV yielded once parsed, excluding the header.
	Rows int
}

// Data is everything a single archive run collected.
type Data struct {
	// Start and End are the inclusive bounds of the archived range.
	Start time.Time
	End   time.Time

	Days       []cronometer.DayTotals
	Servings   []cronometer.ServingEntry
	Exercises  []cronometer.ExerciseEntry
	Biometrics []cronometer.BiometricEntry
	Notes      []cronometer.NoteEntry
	Raw        []RawExport

	// Year is the calendar year the run was asked for, recorded in archive_meta.
	Year int
}

// Stats is the per-table row count of a completed archive.
type Stats struct {
	Days       int
	Servings   int
	Exercises  int
	Biometrics int
	Notes      int
}

// Write creates the SQLite archive at path. It refuses to overwrite an existing file unless force
// is set. The database is built under a temporary name in the same directory and moved into place
// only once it is complete, so an interrupted run never replaces a good archive with a partial one.
func Write(ctx context.Context, path string, force bool, d *Data) (stats Stats, err error) {
	if err = CheckOutput(path, force); err != nil {
		return Stats{}, err
	}

	tmp := path + ".tmp"
	if err = removeIfExists(tmp); err != nil {
		return Stats{}, fmt.Errorf("clearing stale temporary archive %s: %w", tmp, err)
	}

	// Any failure below leaves nothing behind — neither a partial archive at path nor a temp file.
	defer func() {
		if err != nil {
			err = errors.Join(err, removeIfExists(tmp))
		}
	}()

	if stats, err = build(ctx, tmp, d); err != nil {
		return Stats{}, err
	}
	if err = os.Chmod(tmp, archiveMode); err != nil {
		return Stats{}, fmt.Errorf("setting archive permissions: %w", err)
	}
	if err = os.Rename(tmp, path); err != nil {
		return Stats{}, fmt.Errorf("moving archive into place: %w", err)
	}

	return stats, nil
}

// CheckOutput reports whether path is a usable archive destination. Callers run it before doing any
// network work so an already-archived year fails immediately rather than after spending exports
// against Cronometer's daily cap.
func CheckOutput(path string, force bool) error {
	if path == "" {
		return errors.New("no output path given")
	}
	if force {
		return nil
	}

	switch _, err := os.Stat(path); {
	case err == nil:
		return fmt.Errorf("%s already exists (pass --force to overwrite it)", path)
	case errors.Is(err, fs.ErrNotExist):
		return nil
	default:
		return fmt.Errorf("checking output path %s: %w", path, err)
	}
}

// removeIfExists deletes path, treating an already-absent file as success.
func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// build creates the database at path and loads every table in one transaction.
func build(ctx context.Context, path string, d *Data) (stats Stats, err error) {
	ddl, err := DDL()
	if err != nil {
		return Stats{}, err
	}

	db, err := sql.Open(driverName, path)
	if err != nil {
		return Stats{}, fmt.Errorf("opening archive database: %w", err)
	}
	defer func() { err = errors.Join(err, db.Close()) }()

	// The archive is built from scratch and discarded on failure, so durability during the load buys
	// nothing; skipping the per-write fsync loads a year of rows in one pass.
	if _, err = db.ExecContext(ctx, "PRAGMA synchronous = OFF;"); err != nil {
		return Stats{}, fmt.Errorf("configuring archive database: %w", err)
	}
	if _, err = db.ExecContext(ctx, ddl); err != nil {
		return Stats{}, fmt.Errorf("creating archive schema: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Stats{}, fmt.Errorf("starting archive transaction: %w", err)
	}
	// Rollback after a successful Commit reports ErrTxDone, which is not a failure.
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			err = errors.Join(err, rbErr)
		}
	}()

	if err = load(ctx, tx, d); err != nil {
		return Stats{}, err
	}
	if err = tx.Commit(); err != nil {
		return Stats{}, fmt.Errorf("committing archive: %w", err)
	}

	return Stats{
		Days:       len(d.Days),
		Servings:   len(d.Servings),
		Exercises:  len(d.Exercises),
		Biometrics: len(d.Biometrics),
		Notes:      len(d.Notes),
	}, nil
}

// load inserts every table's rows inside tx.
func load(ctx context.Context, tx *sql.Tx, d *Data) error {
	if err := insertMeta(ctx, tx, d); err != nil {
		return err
	}
	if err := insertNutrients(ctx, tx); err != nil {
		return err
	}
	if err := insertDailyNutrition(ctx, tx, d.Days); err != nil {
		return err
	}
	if err := insertServings(ctx, tx, d.Servings); err != nil {
		return err
	}
	if err := insertExercises(ctx, tx, d.Exercises); err != nil {
		return err
	}
	if err := insertBiometrics(ctx, tx, d.Biometrics); err != nil {
		return err
	}
	if err := insertNotes(ctx, tx, d.Notes); err != nil {
		return err
	}
	return insertRawExports(ctx, tx, d.Raw)
}

// insertMeta records how and when the archive was produced, so a file found later explains itself.
func insertMeta(ctx context.Context, tx *sql.Tx, d *Data) (err error) {
	rows := [][2]string{
		{"schema_version", strconv.Itoa(Version)},
		{"generated_at", time.Now().UTC().Format(time.RFC3339)},
		{"year", strconv.Itoa(d.Year)},
		{"range_start", d.Start.Format(cronometer.DateLayout)},
		{"range_end", d.End.Format(cronometer.DateLayout)},
		{"crono_commit", version.CommitHash},
		{"crono_commit_time", version.CommitTime},
		{"crono_built", version.BuildTime},
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO archive_meta (key, value) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("preparing archive_meta insert: %w", err)
	}
	defer func() { err = errors.Join(err, stmt.Close()) }()

	for _, row := range rows {
		if _, err = stmt.ExecContext(ctx, row[0], row[1]); err != nil {
			return fmt.Errorf("inserting archive_meta %q: %w", row[0], err)
		}
	}

	return nil
}

// insertNutrients writes the registry into the database so the generated daily_nutrition columns
// are documented by the file itself.
func insertNutrients(ctx context.Context, tx *sql.Tx) (err error) {
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO nutrient
		(key, column_name, csv_column, unit, display_order, is_default) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("preparing nutrient insert: %w", err)
	}
	defer func() { err = errors.Join(err, stmt.Close()) }()

	for i := range cronometer.Registry {
		n := &cronometer.Registry[i]
		col, colErr := Column(n)
		if colErr != nil {
			return colErr
		}
		if _, err = stmt.ExecContext(ctx, n.Key, col, n.Column, n.Unit, i, boolToInt(n.Default)); err != nil {
			return fmt.Errorf("inserting nutrient %q: %w", n.Key, err)
		}
	}

	return nil
}

// insertDailyNutrition writes one wide row per day. A nutrient the export did not report is stored
// as NULL rather than zero — "not measured" and "measured as zero" are different facts.
func insertDailyNutrition(ctx context.Context, tx *sql.Tx, days []cronometer.DayTotals) (err error) {
	cols, err := Columns()
	if err != nil {
		return err
	}

	// The column list is derived from the compile-time nutrient registry, never from user input;
	// every value is still bound through a placeholder.
	var query strings.Builder
	query.WriteString("INSERT INTO daily_nutrition (date")
	for _, col := range cols {
		query.WriteString(", ")
		query.WriteString(col)
	}
	query.WriteString(") VALUES (?")
	query.WriteString(strings.Repeat(", ?", len(cols)))
	query.WriteString(")")

	stmt, err := tx.PrepareContext(ctx, query.String())
	if err != nil {
		return fmt.Errorf("preparing daily_nutrition insert: %w", err)
	}
	defer func() { err = errors.Join(err, stmt.Close()) }()

	args := make([]any, 0, len(cols)+1)
	for _, day := range days {
		date := day.Date.Format(cronometer.DateLayout)
		args = append(args[:0], any(date))
		for _, n := range cronometer.Registry {
			if v, ok := day.Values[n.Key]; ok {
				args = append(args, v)
			} else {
				args = append(args, nil)
			}
		}
		if _, err = stmt.ExecContext(ctx, args...); err != nil {
			return fmt.Errorf("inserting daily nutrition for %s: %w", date, err)
		}
	}

	return nil
}

func insertServings(ctx context.Context, tx *sql.Tx, servings []cronometer.ServingEntry) (err error) {
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO serving
		(date, time, meal, food, amount, category) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("preparing serving insert: %w", err)
	}
	defer func() { err = errors.Join(err, stmt.Close()) }()

	for _, s := range servings {
		date := s.Date.Format(cronometer.DateLayout)
		if _, err = stmt.ExecContext(ctx, date, s.Time, s.Meal, s.Food, s.Amount, s.Category); err != nil {
			return fmt.Errorf("inserting serving for %s: %w", date, err)
		}
	}

	return nil
}

func insertExercises(ctx context.Context, tx *sql.Tx, exercises []cronometer.ExerciseEntry) (err error) {
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO exercise
		(date, name, minutes, calories) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("preparing exercise insert: %w", err)
	}
	defer func() { err = errors.Join(err, stmt.Close()) }()

	for _, e := range exercises {
		date := e.Date.Format(cronometer.DateLayout)
		if _, err = stmt.ExecContext(ctx, date, e.Name, e.Minutes, e.Calories); err != nil {
			return fmt.Errorf("inserting exercise for %s: %w", date, err)
		}
	}

	return nil
}

// insertBiometrics stores each reading alongside its source-suffix-stripped name, so queries can
// say base_metric = 'Weight' instead of matching 'Weight (Apple Health)' with LIKE.
func insertBiometrics(ctx context.Context, tx *sql.Tx, biometrics []cronometer.BiometricEntry) (err error) {
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO biometric
		(date, measured_at, metric, base_metric, unit, value) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("preparing biometric insert: %w", err)
	}
	defer func() { err = errors.Join(err, stmt.Close()) }()

	for _, b := range biometrics {
		date := b.Date.Format(cronometer.DateLayout)
		measuredAt := b.Time.Format(timestampLayout)
		base := cronometer.BaseMetric(b.Metric)
		if _, err = stmt.ExecContext(ctx, date, measuredAt, b.Metric, base, b.Unit, b.Value); err != nil {
			return fmt.Errorf("inserting biometric for %s: %w", date, err)
		}
	}

	return nil
}

func insertNotes(ctx context.Context, tx *sql.Tx, notes []cronometer.NoteEntry) (err error) {
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO note (date, text) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("preparing note insert: %w", err)
	}
	defer func() { err = errors.Join(err, stmt.Close()) }()

	for _, n := range notes {
		date := n.Date.Format(cronometer.DateLayout)
		if _, err = stmt.ExecContext(ctx, date, n.Text); err != nil {
			return fmt.Errorf("inserting note for %s: %w", date, err)
		}
	}

	return nil
}

// insertRawExports stores each export body verbatim. Cronometer's CSVs carry columns the parsers
// ignore; keeping the originals means the archive can be re-derived without another export.
func insertRawExports(ctx context.Context, tx *sql.Tx, raws []RawExport) (err error) {
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO raw_export
		(kind, csv, byte_count, row_count) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("preparing raw_export insert: %w", err)
	}
	defer func() { err = errors.Join(err, stmt.Close()) }()

	for _, r := range raws {
		if _, err = stmt.ExecContext(ctx, r.Kind, r.CSV, len(r.CSV), r.Rows); err != nil {
			return fmt.Errorf("inserting raw export %q: %w", r.Kind, err)
		}
	}

	return nil
}

// boolToInt renders a bool for a STRICT INTEGER column.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
