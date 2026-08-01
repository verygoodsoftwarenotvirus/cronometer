package cronometer

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// DateLayout is the date format Cronometer uses in the daily-summary export.
const DateLayout = "2006-01-02"

// DayTotals holds one day's nutrient totals, keyed by nutrient Key.
type DayTotals struct {
	Date   time.Time
	Values map[string]float64
	// Weight and BodyFat are the day's body metrics, populated by AttachBodyMetrics when the caller
	// opts in. Each is nil for a day with no matching reading (these come from the biometrics export,
	// not daily nutrition).
	Weight  *Measurement
	BodyFat *Measurement
}

// Measurement is a single biometric value and its unit (e.g. 172.5 "lbs", 18 "%").
type Measurement struct {
	Unit  string
	Value float64
}

// Base biometric metric names Cronometer reports. Real exports suffix the data source, e.g.
// "Weight (Apple Health)"; matching is done on the base name via BaseMetric.
const (
	WeightMetric  = "Weight"
	BodyFatMetric = "Body Fat"
)

// BaseMetric strips the trailing " (source)" that Cronometer appends to biometric metric names
// ("Weight (Apple Health)" -> "Weight", "Body Fat (Withings)" -> "Body Fat").
func BaseMetric(metric string) string {
	base := strings.TrimSpace(metric)
	if strings.HasSuffix(base, ")") {
		if i := strings.LastIndex(base, " ("); i >= 0 {
			base = strings.TrimSpace(base[:i])
		}
	}
	return base
}

// isBodyMetric reports whether metric is a weight or body-fat reading (ignoring the source suffix).
func isBodyMetric(metric string) bool {
	base := BaseMetric(metric)
	return strings.EqualFold(base, WeightMetric) || strings.EqualFold(base, BodyFatMetric)
}

// AttachBodyMetrics fills each day's Weight and BodyFat from the biometric readings, using only the
// first reading of that day (Cronometer/Apple Health can log several per day, from several sources;
// every reading after the first is disregarded). Days with no matching reading are left nil, and
// biometric days absent from days are ignored — these are add-on columns to the nutrition rows.
func AttachBodyMetrics(days []DayTotals, biometrics []BiometricEntry) {
	weights := firstBodyReading(biometrics, WeightMetric)
	bodyFat := firstBodyReading(biometrics, BodyFatMetric)
	for i := range days {
		key := days[i].Date.Format(DateLayout)
		if m, ok := weights[key]; ok {
			v := m
			days[i].Weight = &v
		}
		if m, ok := bodyFat[key]; ok {
			v := m
			days[i].BodyFat = &v
		}
	}
}

// firstBodyReading returns the first (earliest) reading per day, keyed by DateLayout, for the
// biometric whose base name equals metricName (case-insensitively, ignoring the source suffix).
// Same-day readings are ordered by their timestamp; ties keep the first one seen in the export.
func firstBodyReading(biometrics []BiometricEntry, metricName string) map[string]Measurement {
	type reading struct {
		at    time.Time
		unit  string
		value float64
	}
	byDay := make(map[string]reading)
	for _, b := range biometrics {
		if !strings.EqualFold(BaseMetric(b.Metric), metricName) {
			continue
		}
		key := b.Date.Format(DateLayout)
		if cur, ok := byDay[key]; ok && !b.Time.Before(cur.at) {
			continue
		}
		byDay[key] = reading{at: b.Time, value: b.Value, unit: b.Unit}
	}

	out := make(map[string]Measurement, len(byDay))
	for key, r := range byDay {
		out[key] = Measurement{Value: r.value, Unit: r.unit}
	}
	return out
}

// ParseDailyNutrition parses Cronometer's daily-summary CSV into per-day totals. It is
// header-driven: it matches each nutrient by its registry Column and tolerates unknown or
// missing columns (Cronometer adds nutrients over time). The date column may be "Date" or "Day".
func ParseDailyNutrition(raw string) ([]DayTotals, error) {
	r := csv.NewReader(strings.NewReader(raw))
	r.FieldsPerRecord = -1

	header, err := r.Read()
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading CSV header: %w", err)
	}

	colIndex := make(map[string]int, len(header))
	for i, name := range header {
		colIndex[name] = i
	}

	dateIdx, ok := colIndex["Date"]
	if !ok {
		if dateIdx, ok = colIndex["Day"]; !ok {
			return nil, fmt.Errorf("no Date/Day column in export header")
		}
	}

	// Pre-resolve which registry nutrients are present and at what index.
	present := presentNutrientCols(colIndex)

	var days []DayTotals
	for {
		record, readErr := r.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("reading CSV row: %w", readErr)
		}

		if dateIdx >= len(record) {
			return nil, fmt.Errorf("row missing date column")
		}
		date, parseErr := time.Parse(DateLayout, strings.TrimSpace(record[dateIdx]))
		if parseErr != nil {
			return nil, fmt.Errorf("parsing date %q: %w", record[dateIdx], parseErr)
		}

		values := make(map[string]float64, len(present))
		for _, c := range present {
			if c.idx >= len(record) {
				continue
			}
			f, valErr := parseValue(record[c.idx])
			if valErr != nil {
				return nil, fmt.Errorf("parsing %s value %q: %w", c.key, record[c.idx], valErr)
			}
			values[c.key] = f
		}

		days = append(days, DayTotals{Date: date, Values: values})
	}

	return days, nil
}

// nutrientCol pairs a registry nutrient Key with the CSV column index it was found at.
type nutrientCol struct {
	key string
	idx int
}

// presentNutrientCols resolves which registry nutrients appear in the given header (column name ->
// index) and at what index, preserving Registry order. Any Cronometer export that shares the
// daily-summary nutrient columns (e.g. the servings export) can reuse this.
func presentNutrientCols(colIndex map[string]int) []nutrientCol {
	var present []nutrientCol
	for _, n := range Registry {
		if idx, found := colIndex[n.Column]; found {
			present = append(present, nutrientCol{key: n.Key, idx: idx})
		}
	}
	return present
}

// parseValue parses a nutrient cell, treating empty as zero.
func parseValue(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}

// SelectionOptions captures the nutrient-selection flags. Precedence: All > Only > (defaults +
// Add). Exclude is always subtracted from the resulting set.
type SelectionOptions struct {
	Only    string
	Add     string
	Exclude string
	All     bool
}

// SelectNutrients computes the ordered, deduplicated set of nutrients to display from the
// selection flags.
func SelectNutrients(opts SelectionOptions) ([]Nutrient, error) {
	var base []Nutrient

	switch {
	case opts.All:
		base = append(base, Registry...)
	case opts.Only != "":
		only, err := ResolveList(opts.Only)
		if err != nil {
			return nil, err
		}
		base = only
	default:
		base = DefaultNutrients()
		if opts.Add != "" {
			add, err := ResolveList(opts.Add)
			if err != nil {
				return nil, err
			}
			base = mergeNutrients(base, add)
		}
	}

	if opts.Exclude != "" {
		excluded, err := ResolveList(opts.Exclude)
		if err != nil {
			return nil, err
		}
		base = subtractNutrients(base, excluded)
	}

	return base, nil
}

// mergeNutrients appends b to a, skipping keys already present in a.
func mergeNutrients(a, b []Nutrient) []Nutrient {
	seen := make(map[string]struct{}, len(a))
	for _, n := range a {
		seen[n.Key] = struct{}{}
	}
	for _, n := range b {
		if _, ok := seen[n.Key]; ok {
			continue
		}
		seen[n.Key] = struct{}{}
		a = append(a, n)
	}
	return a
}

// subtractNutrients returns a with every nutrient in remove filtered out.
func subtractNutrients(a, remove []Nutrient) []Nutrient {
	drop := make(map[string]struct{}, len(remove))
	for _, n := range remove {
		drop[n.Key] = struct{}{}
	}
	out := a[:0:0]
	for _, n := range a {
		if _, ok := drop[n.Key]; ok {
			continue
		}
		out = append(out, n)
	}
	return out
}
