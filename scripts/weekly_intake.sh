#!/usr/bin/env bash
set -euo pipefail

# Weekly intake averages, oldest week first.
# Usage: weekly_intake.sh [-w WEEKS] [-1] [-c] [-n] [-- <extra crono summary flags>]
#
#   -w WEEKS  number of weeks to cover (default: 26, ~6 months)
#   -1        one export for the whole range, aggregated into weeks locally.
#             Costs 1 export instead of WEEKS; implies -c. Use this past ~10 weeks.
#   -c        compact: one row per week (that week's average), as a single table
#   -n        dry run: print the crono commands instead of running them
#
# Without -1 this runs one `crono summary` per week, and each is a separate export
# against Cronometer's ~10/day cap. Export 11 onward fails, and crono misreports the
# refusal as an expired session and re-logs in -- which then trips TOTP_CODE_INCORRECT,
# because 2FA codes are single-use. See the note printed when WEEKS exceeds the cap.
#
#   weekly_intake.sh -w 26 -1
#   weekly_intake.sh -w 8 -c
#   weekly_intake.sh -w 26 -1 -- --only-nutrients protein,fiber

CRONO="${CRONO:-crono}"
WEEKS=26
COMPACT=false
SINGLE=false
DRY_RUN=false

while getopts ":w:c1nh" opt; do
	case "${opt}" in
	w) WEEKS="${OPTARG}" ;;
	c) COMPACT=true ;;
	1) SINGLE=true ;;
	n) DRY_RUN=true ;;
	h)
		sed -n '4,20p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	\?)
		echo "unknown option: -${OPTARG}" >&2
		exit 1
		;;
	:)
		echo "option -${OPTARG} requires an argument" >&2
		exit 1
		;;
	esac
done
shift $((OPTIND - 1))

if ! [[ "${WEEKS}" =~ ^[0-9]+$ ]] || ((WEEKS < 1)); then
	echo "-w must be a positive integer, got: ${WEEKS}" >&2
	exit 1
fi

# -1 aggregates locally, which only produces per-week averages.
[[ "${SINGLE}" == true ]] && COMPACT=true

# BSD date (macOS) and GNU date (Linux) spell relative dates differently.
days_ago() {
	local n="$1"
	if date -v-1d +%F >/dev/null 2>&1; then
		date -v-"${n}"d +%F
	else
		date -d "-${n} days" +%F
	fi
}

# Preflight: `crono totp` resolves the config and derives a 2FA code without touching
# the network, so a credential problem surfaces here rather than mid-loop -- and costs
# no exports. A missing secret is only fatal on a 2FA-enabled account, so warn, don't exit.
if [[ "${DRY_RUN}" == false ]]; then
	if ! preflight="$("${CRONO}" totp 2>&1)"; then
		echo "preflight: ${preflight}" >&2
		echo "           logins will prompt for a 2FA code, or fail if stdin is not a terminal." >&2
		echo "           no exports spent yet; Ctrl-C now if that is not what you want." >&2
	fi
fi

RANGE_END="$(days_ago 0)"

# ---------------------------------------------------------------------------
# -1: a single export over the whole range, bucketed into weeks with sqlite3.
# The bucket is the same window the loop uses -- week i is [END-7i-6, END-7i] --
# so both modes label weeks identically.
# ---------------------------------------------------------------------------
if [[ "${SINGLE}" == true ]]; then
	range_start="$(days_ago $((WEEKS * 7 - 1)))"

	if [[ "${DRY_RUN}" == true ]]; then
		echo "${CRONO} summary --from ${range_start} --to ${RANGE_END} --format csv ${*:-}"
		exit 0
	fi

	tmp_csv="$(mktemp -t weekly_intake)"
	trap 'rm -f "${tmp_csv}"' EXIT
	# crono writes LF (Go's csv.Writer leaves UseCRLF false), but strip any trailing CR
	# anyway: one would end up inside the last column's *name*, and sqlite would then
	# read "Fiber (g)\r" as a string literal and average it to 0.
	"${CRONO}" summary --from "${range_start}" --to "${RANGE_END}" --format csv ${@+"${@}"} |
		tr -d '\r' >"${tmp_csv}"

	# NULLIF(col, '') keeps a blank cell (e.g. a day with no weight reading) out of the
	# mean instead of counting it as zero -- sqlite's CSV import types every column TEXT,
	# and AVG() would otherwise read '' as 0.
	select_list="$(awk -F, -v q="''" 'NR == 1 {
		for (i = 2; i <= NF; i++) printf "%sROUND(AVG(NULLIF(\"%s\", %s)), 1)", (i > 2 ? ", " : ""), $i, q
		exit
	}' "${tmp_csv}")"

	# Week i is [END-7i-6, END-7i], the same window the per-week loop uses, so both
	# modes label weeks identically. Higher w is older, so ORDER BY w DESC is oldest first.
	rows="$(sqlite3 -csv <<-SQL
		.import --csv '${tmp_csv}' d
		SELECT date('${RANGE_END}', '-' || (w * 7 + 6) || ' days') || '..' ||
		       date('${RANGE_END}', '-' || (w * 7) || ' days'),
		       ${select_list}
		FROM (
		  SELECT *, CAST((julianday('${RANGE_END}') - julianday("Date")) / 7 AS INTEGER) AS w
		  FROM d
		  WHERE "Date" LIKE '____-__-__'
		)
		GROUP BY w
		ORDER BY w DESC;
	SQL
	)"

	printf 'week,%s\n%s\n' "$(head -n 1 "${tmp_csv}" | cut -d, -f2-)" "${rows}" | column -t -s,
	exit 0
fi

if ((WEEKS > 10)); then
	echo "note: ${WEEKS} weeks means ${WEEKS} exports, and Cronometer caps them at ~10/day." >&2
	echo "      export 11 onward fails, and crono reports it as a bogus TOTP error." >&2
	echo "      use -1 to cover the whole range in a single export instead." >&2
fi

# Oldest week first: walk the offsets down so the last line printed is the current week.
compact_rows=()

for ((i = WEEKS - 1; i >= 0; i--)); do
	offset=$((i * 7))
	week_end="$(days_ago "${offset}")"
	week_start="$(days_ago "$((offset + 6))")"

	if [[ "${DRY_RUN}" == true ]]; then
		echo "${CRONO} summary --days 7 --offset-days ${offset} --averages ${*:-}"
		continue
	fi

	if [[ "${COMPACT}" == true ]]; then
		# CSV, not table: renderTable pads with spaces and its headers contain spaces
		# ("energy (kcal)"), so there is no reliable column separator to split on.
		# Keep the header from the first week printed; every later row is data.
		csv="$("${CRONO}" summary --days 7 --offset-days "${offset}" --averages --format csv ${@+"${@}"})"
		if ((${#compact_rows[@]} == 0)); then
			compact_rows+=("week,$(head -n 1 <<<"${csv}" | cut -d, -f2-)")
		fi
		compact_rows+=("${week_start}..${week_end},$(grep '^Average,' <<<"${csv}" | cut -d, -f2-)")
		continue
	fi

	echo "=== ${week_start} .. ${week_end} ==="
	"${CRONO}" summary --days 7 --offset-days "${offset}" --averages ${@+"${@}"}
	echo
done

if [[ "${COMPACT}" == true ]] && ((${#compact_rows[@]} > 0)); then
	printf '%s\n' "${compact_rows[@]}" | column -t -s,
fi
