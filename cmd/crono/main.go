package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/verygoodsoftwarenotvirus/cronometer"
	"github.com/verygoodsoftwarenotvirus/cronometer/internal/archive"
	"github.com/verygoodsoftwarenotvirus/cronometer/internal/cronoclient"
	"github.com/verygoodsoftwarenotvirus/cronometer/version"
)

const usageText = `Usage: crono <command> [args]

Commands:
  summary [flags]   Print daily nutrition totals from Cronometer
                    (add --detailed for a per-meal breakdown with activity,
                    biomarkers, and notes)
  archive [flags]   Pull a whole calendar year into a SQLite file
                    (crono archive --year 2025 --out 2025.db)
  totp              Print the current 2FA code from the configured TOTP secret
                    and exit (no login; safe to check against your authenticator)
  version           Print build version info

Run 'crono summary -h' or 'crono archive -h' for their flags.

Credentials:
  Read from ~/.config/crono/config.json (JSON: {"email":"...","password":"..."}),
  overridable via the CRONOMETER_EMAIL / CRONOMETER_PASSWORD environment variables.
  The config file stores the password in plaintext; it is created with 0600 perms.`

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	ctx := context.Background()

	switch os.Args[1] {
	case "summary":
		cmdSummary(ctx, os.Args[2:])
	case "archive":
		cmdArchive(ctx, os.Args[2:])
	case "totp":
		cmdTOTP(os.Args[2:])
	case "version":
		fmt.Printf("commit: %s\nbuilt:  %s\ncommit time: %s\n", version.CommitHash, version.BuildTime, version.CommitTime)
	case "-h", "--help", "help":
		printUsage()
	default:
		warnf("unknown command: %s", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	if _, err := fmt.Fprintln(os.Stderr, usageText); err != nil {
		slog.Error("writing usage", "error", err)
	}
}

// warnf writes a formatted line to stderr.
func warnf(format string, a ...any) {
	if _, err := fmt.Fprintf(os.Stderr, format+"\n", a...); err != nil {
		slog.Error("writing to stderr", "error", err)
	}
}

// cmdTOTP prints the current 2FA code derived from the configured TOTP secret, without contacting
// Cronometer. It's a diagnostic: compare the printed code against your authenticator app to confirm
// the secret is correct, with no risk of tripping Cronometer's login rate-limiting.
func cmdTOTP(args []string) {
	fs := flag.NewFlagSet("totp", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file (default: ~/.config/crono/config.json)")
	if err := fs.Parse(args); err != nil {
		slog.Error("parsing flags", "error", err)
		os.Exit(1)
	}

	path := *configPath
	if path == "" {
		var err error
		path, err = cronometer.DefaultConfigPath()
		if err != nil {
			slog.Error("resolving config path", "error", err)
			os.Exit(1)
		}
	}
	creds, err := cronometer.ResolveCredentials(path)
	if err != nil {
		slog.Error("loading credentials", "error", err)
		os.Exit(1)
	}

	now := time.Now()
	code, present, err := creds.TOTPCode(now)
	if err != nil {
		slog.Error("generating 2FA code", "error", err)
		os.Exit(1)
	}
	if !present {
		warnf("no TOTP secret configured (set CRONOMETER_TOTP_SECRET or totp_secret in config)")
		os.Exit(1)
	}

	// TOTP codes roll on a 30-second boundary; show how long this one stays valid so a fresh
	// login attempt can be timed to a new window (codes are single-use server-side).
	secsLeft := 30 - now.Unix()%30
	fmt.Printf("%s  (valid %ds)\n", code, secsLeft)
}

func cmdSummary(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("summary", flag.ExitOnError)
	days := fs.Int("days", 1, "number of days back through today")
	offsetDays := fs.Int("offset-days", 0, "shift the --days window back this many days (e.g. --days 1 --offset-days 7 is the day one week ago)")
	from := fs.String("from", "", "start date (YYYY-MM-DD); requires --to, overrides --days")
	to := fs.String("to", "", "end date (YYYY-MM-DD); requires --from, overrides --days")
	only := fs.String("only-nutrients", "", "comma-separated nutrients to show exclusively")
	add := fs.String("add-nutrients", "", "comma-separated nutrients to add to the defaults")
	exclude := fs.String("exclude-nutrients", "", "comma-separated nutrients to remove from the set")
	all := fs.Bool("all-nutrients", false, "show every nutrient")
	listNutrients := fs.Bool("list-nutrients", false, "print available nutrient names and exit")
	format := fs.String("format", "table", "output format: table, json, or csv")
	asJSON := fs.Bool("json", false, "shorthand for --format json")
	averages := fs.Bool("averages", false, "append an average row (table/csv)")
	includeWeight := fs.Bool("include-weight", false, "add body-weight and body-fat columns (daily average of readings; non-detailed view only)")
	detailed := fs.Bool("detailed", false, "expand into a per-meal breakdown with activity, biomarkers, and notes (table/json)")
	configPath := fs.String("config", "", "path to config file (default: ~/.config/crono/config.json)")
	if err := fs.Parse(args); err != nil {
		slog.Error("parsing flags", "error", err)
		os.Exit(1)
	}

	if *listNutrients {
		if err := cronometer.RenderNutrientList(os.Stdout); err != nil {
			slog.Error("listing nutrients", "error", err)
			os.Exit(1)
		}
		return
	}

	outFormat, err := cronometer.ParseFormat(*format)
	if err != nil {
		slog.Error("invalid format", "error", err)
		os.Exit(1)
	}
	if *asJSON {
		outFormat = cronometer.FormatJSON
	}
	if *detailed && outFormat == cronometer.FormatCSV {
		slog.Error("detailed output supports only table or json")
		os.Exit(1)
	}

	start, end, err := dateRange(*days, *offsetDays, *from, *to)
	if err != nil {
		slog.Error("invalid date range", "error", err)
		os.Exit(1)
	}

	nutrients, err := cronometer.SelectNutrients(cronometer.SelectionOptions{
		All:     *all,
		Only:    *only,
		Add:     *add,
		Exclude: *exclude,
	})
	if err != nil {
		slog.Error("selecting nutrients", "error", err)
		os.Exit(1)
	}
	if len(nutrients) == 0 {
		slog.Error("no nutrients selected")
		os.Exit(1)
	}

	path := *configPath
	if path == "" {
		path, err = cronometer.DefaultConfigPath()
		if err != nil {
			slog.Error("resolving config path", "error", err)
			os.Exit(1)
		}
	}
	creds, err := cronometer.ResolveCredentials(path)
	if err != nil {
		slog.Error("loading credentials", "error", err)
		os.Exit(1)
	}

	if *detailed {
		renderDetailedSummary(ctx, creds, start, end, outFormat, nutrients)
		return
	}

	var raw, rawBiometrics string
	if err = runWithClient(ctx, creds, func(c *cronoclient.Client) error {
		r, e := c.ExportDailyNutrition(ctx, start, end)
		if e != nil {
			return e
		}
		raw = r
		// --include-weight needs a second export (weight lives in biometrics, not daily nutrition).
		// It shares this one authenticated session but still counts against Cronometer's export cap.
		if *includeWeight {
			if rawBiometrics, e = c.ExportBiometrics(ctx, start, end); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		slog.Error("fetching daily nutrition", "error", err)
		os.Exit(1)
	}

	parsed, err := cronometer.ParseDailyNutrition(raw)
	if err != nil {
		slog.Error("parsing export", "error", err)
		os.Exit(1)
	}

	if *includeWeight {
		biometrics, perr := cronometer.ParseBiometrics(rawBiometrics)
		if perr != nil {
			slog.Error("parsing biometrics", "error", perr)
			os.Exit(1)
		}
		cronometer.AttachBodyMetrics(parsed, biometrics)
	}

	if err = cronometer.Render(os.Stdout, outFormat, parsed, nutrients, *averages); err != nil {
		slog.Error("rendering output", "error", err)
		os.Exit(1)
	}
}

// detailExports holds the raw CSV bodies for the detailed view's data sources.
type detailExports struct {
	daily      string
	servings   string
	exercises  string
	biometrics string
	notes      string
}

// renderDetailedSummary fetches all detail exports over one authenticated session, joins them into
// per-day details, and renders them in the requested format.
func renderDetailedSummary(ctx context.Context, creds *cronometer.Config, start, end time.Time, outFormat cronometer.Format, nutrients []cronometer.Nutrient) {
	var ex detailExports
	if err := runWithClient(ctx, creds, func(c *cronoclient.Client) error {
		var e error
		if ex.daily, e = c.ExportDailyNutrition(ctx, start, end); e != nil {
			return e
		}
		if ex.servings, e = c.ExportServings(ctx, start, end); e != nil {
			return e
		}
		if ex.exercises, e = c.ExportExercises(ctx, start, end); e != nil {
			return e
		}
		if ex.biometrics, e = c.ExportBiometrics(ctx, start, end); e != nil {
			return e
		}
		ex.notes, e = c.ExportNotes(ctx, start, end)
		return e
	}); err != nil {
		slog.Error("fetching detailed export", "error", err)
		os.Exit(1)
	}

	days, err := cronometer.ParseDailyNutrition(ex.daily)
	if err != nil {
		slog.Error("parsing daily summary", "error", err)
		os.Exit(1)
	}
	servings, err := cronometer.ParseServings(ex.servings)
	if err != nil {
		slog.Error("parsing servings", "error", err)
		os.Exit(1)
	}
	exercises, err := cronometer.ParseExercises(ex.exercises)
	if err != nil {
		slog.Error("parsing exercises", "error", err)
		os.Exit(1)
	}
	biometrics, err := cronometer.ParseBiometrics(ex.biometrics)
	if err != nil {
		slog.Error("parsing biometrics", "error", err)
		os.Exit(1)
	}
	notes, err := cronometer.ParseNotes(ex.notes)
	if err != nil {
		slog.Error("parsing notes", "error", err)
		os.Exit(1)
	}

	details := cronometer.BuildDayDetails(days, servings, exercises, biometrics, notes)
	if err = cronometer.RenderDetailed(os.Stdout, outFormat, details, nutrients); err != nil {
		slog.Error("rendering output", "error", err)
		os.Exit(1)
	}
}

// cmdArchive pulls a whole calendar year out of Cronometer and writes it to a SQLite file whose
// schema this app owns (see internal/archive). It costs five of Cronometer's ~10 daily exports.
func cmdArchive(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("archive", flag.ExitOnError)
	year := fs.Int("year", 0, "calendar year to archive (e.g. 2025)")
	out := fs.String("out", "", "output SQLite file (default: crono-<year>.db)")
	force := fs.Bool("force", false, "overwrite the output file if it already exists")
	configPath := fs.String("config", "", "path to config file (default: ~/.config/crono/config.json)")
	if err := fs.Parse(args); err != nil {
		slog.Error("parsing flags", "error", err)
		os.Exit(1)
	}

	start, end, err := yearRange(*year)
	if err != nil {
		slog.Error("invalid year", "error", err)
		os.Exit(1)
	}

	outPath := *out
	if outPath == "" {
		outPath = fmt.Sprintf("crono-%d.db", *year)
	}
	// Check the destination before authenticating: discovering it is unusable after spending five
	// exports against Cronometer's daily cap would cost the rest of the day's budget for nothing.
	if err = archive.CheckOutput(outPath, *force); err != nil {
		slog.Error("cannot write archive", "error", err)
		os.Exit(1)
	}

	path := *configPath
	if path == "" {
		path, err = cronometer.DefaultConfigPath()
		if err != nil {
			slog.Error("resolving config path", "error", err)
			os.Exit(1)
		}
	}
	creds, err := cronometer.ResolveCredentials(path)
	if err != nil {
		slog.Error("loading credentials", "error", err)
		os.Exit(1)
	}

	var ex detailExports
	if err = runWithClient(ctx, creds, func(c *cronoclient.Client) error {
		var e error
		if ex.daily, e = c.ExportDailyNutrition(ctx, start, end); e != nil {
			return e
		}
		if ex.servings, e = c.ExportServings(ctx, start, end); e != nil {
			return e
		}
		if ex.exercises, e = c.ExportExercises(ctx, start, end); e != nil {
			return e
		}
		if ex.biometrics, e = c.ExportBiometrics(ctx, start, end); e != nil {
			return e
		}
		ex.notes, e = c.ExportNotes(ctx, start, end)
		return e
	}); err != nil {
		slog.Error("fetching exports", "error", err)
		os.Exit(1)
	}

	data, err := buildArchiveData(*year, start, end, &ex)
	if err != nil {
		slog.Error("parsing exports", "error", err)
		os.Exit(1)
	}

	stats, err := archive.Write(ctx, outPath, *force, data)
	if err != nil {
		slog.Error("writing archive", "error", err)
		os.Exit(1)
	}

	fmt.Printf("wrote %s (%d days, %d servings, %d exercises, %d biometrics, %d notes)\n",
		outPath, stats.Days, stats.Servings, stats.Exercises, stats.Biometrics, stats.Notes)
}

// buildArchiveData parses every export body and pairs it with the CSV it came from, so the archive
// holds both the structured tables and the originals behind them.
func buildArchiveData(year int, start, end time.Time, ex *detailExports) (*archive.Data, error) {
	days, err := cronometer.ParseDailyNutrition(ex.daily)
	if err != nil {
		return nil, fmt.Errorf("parsing daily summary: %w", err)
	}
	servings, err := cronometer.ParseServings(ex.servings)
	if err != nil {
		return nil, fmt.Errorf("parsing servings: %w", err)
	}
	exercises, err := cronometer.ParseExercises(ex.exercises)
	if err != nil {
		return nil, fmt.Errorf("parsing exercises: %w", err)
	}
	biometrics, err := cronometer.ParseBiometrics(ex.biometrics)
	if err != nil {
		return nil, fmt.Errorf("parsing biometrics: %w", err)
	}
	notes, err := cronometer.ParseNotes(ex.notes)
	if err != nil {
		return nil, fmt.Errorf("parsing notes: %w", err)
	}

	return &archive.Data{
		Year:       year,
		Start:      start,
		End:        end,
		Days:       days,
		Servings:   servings,
		Exercises:  exercises,
		Biometrics: biometrics,
		Notes:      notes,
		Raw: []archive.RawExport{
			{Kind: "dailySummary", CSV: ex.daily, Rows: len(days)},
			{Kind: "servings", CSV: ex.servings, Rows: len(servings)},
			{Kind: "exercises", CSV: ex.exercises, Rows: len(exercises)},
			{Kind: "biometrics", CSV: ex.biometrics, Rows: len(biometrics)},
			{Kind: "notes", CSV: ex.notes, Rows: len(notes)},
		},
	}, nil
}

// yearRange returns the inclusive [Jan 1, Dec 31] bounds of a calendar year, in the machine's local
// time zone so it matches how --from/--to are interpreted elsewhere.
func yearRange(year int) (start, end time.Time, err error) {
	if year == 0 {
		return start, end, fmt.Errorf("--year is required (e.g. --year %d)", time.Now().Year())
	}
	if maxYear := time.Now().Year() + 1; year < 2000 || year > maxYear {
		return start, end, fmt.Errorf("--year must be between 2000 and %d", maxYear)
	}

	loc := time.Now().Location()
	start = time.Date(year, time.January, 1, 0, 0, 0, 0, loc)
	end = time.Date(year, time.December, 31, 0, 0, 0, 0, loc)

	return start, end, nil
}

// runWithClient invokes fn with an authenticated client, preferring a cached session and only
// performing a full (2FA) login when no valid session exists. It probes the cached session by
// running fn; if that fails, it logs in fresh (caching the new session) and runs fn once more.
// Caching the session means 2FA isn't re-run on every invocation — and repeated runs don't trip
// Cronometer's login rate-limiting. fn may issue multiple exports so they share one login.
func runWithClient(ctx context.Context, creds *cronometer.Config, fn func(*cronoclient.Client) error) error {
	sessionPath, err := cronometer.DefaultSessionPath()
	if err != nil {
		return err
	}

	// Probe the cached session on its own client: RestoreSession seeds the cookie jar with the
	// (possibly expired) sesnonce and populates the nonce/userID fields. If the probe fails we
	// must NOT reuse this client for the login — the stale sesnonce cookie would ride along on the
	// login request, and the pre-set nonce would defeat Login's "did a fresh session establish?"
	// guard. So the login below gets a clean client instead.
	if sess, loadErr := cronoclient.LoadSession(sessionPath); loadErr == nil {
		probe := cronoclient.NewClient(nil)
		probe.RestoreSession(sess)
		if runErr := fn(probe); runErr == nil {
			return nil
		}
		slog.Info("cached session invalid or expired; logging in again")
	}

	client := cronoclient.NewClient(nil)
	code, err := twoFactorCode(creds)
	if err != nil {
		return err
	}
	if err = client.Login(ctx, creds.Email, creds.Password, code); err != nil {
		return err
	}
	if err = cronoclient.SaveSession(sessionPath, client.Session()); err != nil {
		slog.Warn("could not cache session", "error", err)
	}

	return fn(client)
}

// twoFactorCode returns the 2FA code: generated from a configured TOTP secret, or read from the
// terminal otherwise. A blank line is allowed for accounts without 2FA.
func twoFactorCode(creds *cronometer.Config) (string, error) {
	code, present, err := creds.TOTPCode(time.Now())
	if err != nil {
		return "", fmt.Errorf("generating 2FA code: %w", err)
	}
	if present {
		return code, nil
	}

	if _, werr := fmt.Fprint(os.Stderr, "Cronometer 2FA code (blank if 2FA is disabled): "); werr != nil {
		return "", fmt.Errorf("writing prompt: %w", werr)
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading 2FA code: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// dateRange computes the inclusive [start, end] range. Explicit from/to (both required) override
// the days-back window; offsetDays shifts that window back so --days 1 --offset-days 7 is the
// single day one week ago. Dates are interpreted in the machine's local time zone.
func dateRange(days, offsetDays int, from, to string) (start, end time.Time, err error) {
	loc := time.Now().Location()

	if from != "" || to != "" {
		if from == "" || to == "" {
			return start, end, fmt.Errorf("--from and --to must be used together")
		}
		if offsetDays != 0 {
			return start, end, fmt.Errorf("--offset-days cannot be combined with --from/--to")
		}
		start, err = time.ParseInLocation(cronometer.DateLayout, from, loc)
		if err != nil {
			return start, end, fmt.Errorf("parsing --from: %w", err)
		}
		end, err = time.ParseInLocation(cronometer.DateLayout, to, loc)
		if err != nil {
			return start, end, fmt.Errorf("parsing --to: %w", err)
		}
		if end.Before(start) {
			return start, end, fmt.Errorf("--to is before --from")
		}
		return start, end, nil
	}

	if days < 1 {
		return start, end, fmt.Errorf("--days must be >= 1")
	}
	if offsetDays < 0 {
		return start, end, fmt.Errorf("--offset-days must be >= 0")
	}
	now := time.Now()
	end = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -offsetDays)
	start = end.AddDate(0, 0, -(days - 1))
	return start, end, nil
}
