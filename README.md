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
