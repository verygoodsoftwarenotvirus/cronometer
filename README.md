# crono

A small CLI for pulling your eating data out of [Cronometer](https://cronometer.com) from the
terminal — a nutrient swiss-army knife built around `crono summary`.

```console
$ crono summary --days 3 --add-nutrients fiber,caffeine
$ crono summary --only-nutrients protein --days 1
$ crono summary --days 3 --only-nutrients protein,fiber --json
```

## Install

```console
make build            # compiles all packages
go build ./cmd/crono  # or just the binary
```

## Credentials

Cronometer has **no official API**, so `crono` logs in with your account email and password.

Provide them either way:

- Config file at `~/.config/crono/config.json` (created `0600`):

  ```json
  { "email": "you@example.com", "password": "..." }
  ```

- Environment variables (override the file), handy with `op run` / SOPS so the password never
  touches disk:

  ```console
  export CRONOMETER_EMAIL=you@example.com
  export CRONOMETER_PASSWORD="$(op read 'op://Personal/Cronometer/password')"
  ```

The config file stores secrets in plaintext — protect it accordingly (it's written `0600`).

### Two-factor authentication

If your account has 2FA (TOTP) enabled, `crono` needs the 6-digit code at login. Either:

- **Store the TOTP secret** (the base32 key from authenticator setup) so `crono` generates codes
  itself — fully non-interactive, good for scripts/cron:

  ```json
  { "email": "...", "password": "...", "totp_secret": "JBSWY3DPEHPK3PXP" }
  ```

  or `export CRONOMETER_TOTP_SECRET=...`. (This makes the file's secrecy your only protection —
  guard it like the password.)

- **Or do nothing** and `crono` prompts for the current code at the terminal when a login is
  needed.

After a successful login the session is cached to `~/.config/crono/session.json` (Cronometer
sessions last ~14 days), so you only re-authenticate — and re-enter 2FA — when it expires. This
also avoids Cronometer's aggressive login rate-limiting on repeated runs.

## `crono summary`

| Flag | Default | Meaning |
|------|---------|---------|
| `--days N` | `1` | last N days through today |
| `--offset-days N` | `0` | shift the `--days` window back N days (`--days 1 --offset-days 7` is the day one week ago) |
| `--from` / `--to` | — | explicit `YYYY-MM-DD` range (both required; overrides `--days`) |
| `--only-nutrients a,b` | — | show exactly these |
| `--add-nutrients a,b` | — | defaults plus these |
| `--exclude-nutrients a,b` | — | remove these from the set |
| `--all-nutrients` | | every nutrient |
| `--list-nutrients` | | print available nutrient names and exit |
| `--format table\|json\|csv` | `table` | output format |
| `--json` | | shorthand for `--format json` |
| `--averages` | | append an average row (table/csv) |
| `--include-weight` | | add body-weight and body-fat columns (daily average of readings) |

Nutrient names are case-insensitive and accept aliases (`calories`→energy, `carbs`→carbohydrates,
`vit-c`→vitamin-c, …). Run `crono summary --list-nutrients` for the full set.

`--include-weight` adds body-weight and body-fat columns sourced from the biometrics export
(averaging that day's readings, in your account's units); a column is shown only if at least one day
in the range has a reading, and days without one show blank. Metrics are matched by base name, so
source-tagged exports like `Weight (Apple Health)` and `Body Fat (Apple Health)` are picked up. It
costs one extra export per run against Cronometer's ~10/day cap, and is ignored under `--detailed`
(which already lists these under Biomarkers).

## `crono archive`

Pull a whole calendar year into a SQLite file and query it locally forever after — no network, no
rate limit.

```console
$ crono archive --year 2025 --out 2025.db
wrote 2025.db (365 days, 4812 servings, 210 exercises, 1043 biometrics, 22 notes)
```

| Flag | Default | Meaning |
|------|---------|---------|
| `--year N` | — | required; the calendar year to archive |
| `--out FILE` | `crono-<year>.db` | output path |
| `--force` | | overwrite `--out` if it already exists (otherwise `crono` refuses) |
| `--config PATH` | | as elsewhere |

One run costs **five** of Cronometer's ~10 daily exports (one per data kind), all over a single
login. The destination is checked *before* authenticating, so an existing file fails immediately
rather than after spending that budget. The database is built under a temporary name and moved into
place only when complete, so an interrupted run never replaces a good archive with a partial one.
The file is written `0600` — it holds a year of personal health data.

### Schema

`crono` owns this schema; the nutrient columns are generated from the same registry that drives
`crono summary --list-nutrients`, so the two cannot drift.

| Table | Contents |
|-------|----------|
| `daily_nutrition` | one row per day, `date` plus one `REAL` column per nutrient (`energy_kcal`, `protein_g`, `vitamin_a_ug`, …) |
| `serving` | every logged food: `date`, `time`, `meal`, `food`, `amount`, `category` |
| `exercise` | `date`, `name`, `minutes`, `calories` |
| `biometric` | `date`, `measured_at`, `metric`, `base_metric`, `unit`, `value` |
| `note` | `date`, `text` |
| `nutrient` | the registry itself — key, column name, original CSV header, unit |
| `archive_meta` | schema version, generation time, archived range, `crono` build info |
| `raw_export` | each export's CSV stored verbatim, so nothing Cronometer sent is lost |

A nutrient the export didn't report is `NULL`, not `0` — "not measured" and "measured as zero" stay
distinguishable. `base_metric` is the source-suffix-stripped metric name, so `base_metric = 'Weight'`
matches `Weight (Apple Health)` without a `LIKE`.

```console
$ sqlite3 2025.db 'SELECT date, energy_kcal, protein_g FROM daily_nutrition ORDER BY date LIMIT 5;'
$ sqlite3 2025.db 'SELECT meal, COUNT(*) FROM serving GROUP BY meal ORDER BY 2 DESC;'
$ sqlite3 2025.db "SELECT date, AVG(value) FROM biometric WHERE base_metric='Weight' GROUP BY date;"
$ sqlite3 2025.db "SELECT food, COUNT(*) c FROM serving GROUP BY food ORDER BY c DESC LIMIT 10;"
```

Two things `daily_nutrition` and `serving` do not carry, both recoverable from `raw_export`:

- **Nutrients outside the registry.** Cronometer's daily summary currently also reports Oxalate,
  Phytate, Insoluble/Soluble Fiber, Added Sugars, and the fatty-acid breakdown (ALA, DHA, EPA, AA,
  LA). None are in `Registry` yet, so they get no column — add them to `nutrients.go` and they
  appear in the next archive (and in `crono summary`).
- **Per-food nutrient values.** The servings parser captures only the food list and meal grouping.

Expect a sizeable file if you sync biometrics from Apple Health: a year of per-minute heart-rate
samples is a few hundred thousand `biometric` rows, and the verbatim CSV in `raw_export` roughly
doubles it. Drop the `raw_export` row and `VACUUM` if you want it smaller.

## Caveats

- **Unofficial API.** `crono` drives the same reverse-engineered GWT-RPC + CSV-export flow the web
  app uses. When Cronometer ships a web update, login can break; the fix is usually updating the
  GWT "magic values" in [`internal/cronoclient/client.go`](internal/cronoclient/client.go)
  (`defaultGWTPermutation` / `defaultGWTHeader`), which can also be overridden at runtime via
  `ClientOptions`.
- **Rate limit.** Cronometer caps exports at ~10/day for personal use.

## License

GPL-2.0. The GWT transport in `internal/cronoclient/` is adapted from
[`github.com/burke/gocronometer`](https://github.com/burke/gocronometer) (and the original
[`jrmycanady/gocronometer`](https://github.com/jrmycanady/gocronometer)), which are GPL-2.0; this
project is a derivative and is licensed the same. See [LICENSE](LICENSE).
