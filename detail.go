package cronometer

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// ServingEntry is one logged food-diary entry from the "servings" export. Cronometer's servings
// export carries only the food list and meal grouping — per-serving nutrient values are not
// included (day-level nutrition comes from the daily-summary export instead).
type ServingEntry struct {
	Date     time.Time
	Time     string
	Meal     string
	Food     string
	Amount   string
	Category string
}

// ExerciseEntry is one logged activity from the "exercises" export.
type ExerciseEntry struct {
	Date     time.Time
	Name     string
	Minutes  float64
	Calories float64
}

// BiometricEntry is one logged measurement from the "biometrics" export (weight, blood pressure,
// etc.).
type BiometricEntry struct {
	Date   time.Time
	Metric string
	Unit   string
	Value  float64
}

// NoteEntry is one free-text diary note from the "notes" export.
type NoteEntry struct {
	Date time.Time
	Text string
}

// canonicalMealOrder is the display order for the standard Cronometer meal groups. Unrecognized
// groups sort after these, in first-seen order.
var canonicalMealOrder = []string{"Breakfast", "Lunch", "Dinner", "Snacks"}

func mealRank(name string) int {
	for i, m := range canonicalMealOrder {
		if strings.EqualFold(name, m) {
			return i
		}
	}
	return len(canonicalMealOrder)
}

// readTable reads a CSV export into a trimmed column-name->index map and the remaining data rows.
// A completely empty body yields (nil, nil, nil) so callers can treat "no data" uniformly.
func readTable(raw string) (colIndex map[string]int, rows [][]string, err error) {
	r := csv.NewReader(strings.NewReader(raw))
	r.FieldsPerRecord = -1

	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading CSV header: %w", err)
	}

	colIndex = make(map[string]int, len(header))
	for i, name := range header {
		colIndex[strings.TrimSpace(name)] = i
	}

	rows, err = r.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("reading CSV rows: %w", err)
	}
	return colIndex, rows, nil
}

// firstCol returns the index of the first of names present in colIndex.
func firstCol(colIndex map[string]int, names ...string) (int, bool) {
	for _, n := range names {
		if i, ok := colIndex[n]; ok {
			return i, true
		}
	}
	return -1, false
}

// cell returns the trimmed value at idx, or "" if idx is out of range.
func cell(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

// cellFloat parses the value at idx, treating empty/out-of-range as zero.
func cellFloat(row []string, idx int) (float64, error) {
	return parseValue(cell(row, idx))
}

// dateColumn resolves the Date/Day column, tolerating that an empty export has no header at all.
// It returns ok=false (no error) when there are no rows to place, so empty exports parse cleanly.
func dateColumn(colIndex map[string]int, rowCount int) (idx int, ok bool, err error) {
	if i, found := firstCol(colIndex, "Date", "Day"); found {
		return i, true, nil
	}
	if rowCount == 0 {
		return -1, false, nil
	}
	return -1, false, fmt.Errorf("no Date/Day column in export header")
}

// parseDate parses a Cronometer date cell, tolerating a trailing time component (e.g.
// "2025-06-27 08:00") by reading only the leading date.
func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(DateLayout, s); err == nil {
		return t, nil
	}
	if len(s) >= len(DateLayout) {
		if t, err := time.Parse(DateLayout, s[:len(DateLayout)]); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("parsing date %q", s)
}

// ParseServings parses Cronometer's "servings" CSV into per-food entries. It is header-driven and
// tolerant: the meal, food, and amount columns are matched against candidate names, and nutrient
// columns reuse the daily-summary registry matching. Unknown columns are ignored.
func ParseServings(raw string) ([]ServingEntry, error) {
	colIndex, rows, err := readTable(raw)
	if err != nil {
		return nil, err
	}
	dateIdx, ok, err := dateColumn(colIndex, len(rows))
	if err != nil || !ok {
		return nil, err
	}

	timeIdx, _ := firstCol(colIndex, "Time")
	mealIdx, _ := firstCol(colIndex, "Group")
	foodIdx, _ := firstCol(colIndex, "Food Name", "Food")
	amountIdx, _ := firstCol(colIndex, "Amount", "Quantity")
	categoryIdx, _ := firstCol(colIndex, "Category")

	entries := make([]ServingEntry, 0, len(rows))
	for _, row := range rows {
		date, perr := parseDate(cell(row, dateIdx))
		if perr != nil {
			return nil, perr
		}
		meal := cell(row, mealIdx)
		if meal == "" {
			meal = "Uncategorized"
		}
		entries = append(entries, ServingEntry{
			Date:     date,
			Time:     cell(row, timeIdx),
			Meal:     meal,
			Food:     cell(row, foodIdx),
			Amount:   cell(row, amountIdx),
			Category: cell(row, categoryIdx),
		})
	}
	return entries, nil
}

// ParseExercises parses Cronometer's "exercises" CSV into activity entries.
func ParseExercises(raw string) ([]ExerciseEntry, error) {
	colIndex, rows, err := readTable(raw)
	if err != nil {
		return nil, err
	}
	dateIdx, ok, err := dateColumn(colIndex, len(rows))
	if err != nil || !ok {
		return nil, err
	}

	nameIdx, _ := firstCol(colIndex, "Exercise", "Activity", "Name")
	minutesIdx, _ := firstCol(colIndex, "Minutes", "Duration (min)", "Duration")
	calIdx, _ := firstCol(colIndex, "Calories Burned", "Energy (kcal)", "Calories")

	entries := make([]ExerciseEntry, 0, len(rows))
	for _, row := range rows {
		date, perr := parseDate(cell(row, dateIdx))
		if perr != nil {
			return nil, perr
		}
		minutes, merr := cellFloat(row, minutesIdx)
		if merr != nil {
			return nil, fmt.Errorf("parsing exercise minutes: %w", merr)
		}
		cals, cerr := cellFloat(row, calIdx)
		if cerr != nil {
			return nil, fmt.Errorf("parsing exercise calories: %w", cerr)
		}
		entries = append(entries, ExerciseEntry{
			Date:     date,
			Name:     cell(row, nameIdx),
			Minutes:  minutes,
			Calories: cals,
		})
	}
	return entries, nil
}

// ParseBiometrics parses Cronometer's "biometrics" CSV into measurement entries.
func ParseBiometrics(raw string) ([]BiometricEntry, error) {
	colIndex, rows, err := readTable(raw)
	if err != nil {
		return nil, err
	}
	dateIdx, ok, err := dateColumn(colIndex, len(rows))
	if err != nil || !ok {
		return nil, err
	}

	metricIdx, _ := firstCol(colIndex, "Metric", "Group", "Measurement")
	unitIdx, _ := firstCol(colIndex, "Unit", "Units")
	valueIdx, _ := firstCol(colIndex, "Amount", "Value")

	entries := make([]BiometricEntry, 0, len(rows))
	for _, row := range rows {
		date, perr := parseDate(cell(row, dateIdx))
		if perr != nil {
			return nil, perr
		}
		value, verr := cellFloat(row, valueIdx)
		if verr != nil {
			return nil, fmt.Errorf("parsing biometric value: %w", verr)
		}
		entries = append(entries, BiometricEntry{
			Date:   date,
			Metric: cell(row, metricIdx),
			Unit:   cell(row, unitIdx),
			Value:  value,
		})
	}
	return entries, nil
}

// ParseNotes parses Cronometer's "notes" CSV into note entries.
func ParseNotes(raw string) ([]NoteEntry, error) {
	colIndex, rows, err := readTable(raw)
	if err != nil {
		return nil, err
	}
	dateIdx, ok, err := dateColumn(colIndex, len(rows))
	if err != nil || !ok {
		return nil, err
	}

	textIdx, _ := firstCol(colIndex, "Note", "Notes", "Comment")

	entries := make([]NoteEntry, 0, len(rows))
	for _, row := range rows {
		date, perr := parseDate(cell(row, dateIdx))
		if perr != nil {
			return nil, perr
		}
		text := cell(row, textIdx)
		if text == "" {
			continue
		}
		entries = append(entries, NoteEntry{Date: date, Text: text})
	}
	return entries, nil
}

// MealDetail is one meal group within a day and the foods logged to it.
type MealDetail struct {
	Name  string
	Foods []ServingEntry
}

// BiometricSummary aggregates all same-metric measurements on a day. Biometric sources (e.g. Apple
// Health) can log hundreds of samples per metric per day, so the detailed view collapses them into
// count + min/avg/max rather than listing every reading.
type BiometricSummary struct {
	Metric string
	Unit   string
	Count  int
	Min    float64
	Max    float64
	Avg    float64
	Last   float64
}

// DayDetail is everything logged on one calendar day: meals (each with foods), activity,
// biomarkers, notes, and the day's nutrient totals from the daily-summary export.
type DayDetail struct {
	Totals     map[string]float64
	Date       time.Time
	Meals      []MealDetail
	Exercises  []ExerciseEntry
	Biometrics []BiometricSummary
	Notes      []NoteEntry
}

// BuildDayDetails joins the daily-summary totals with servings, exercises, biometrics, and notes
// into one ordered slice of per-day details (ascending by date). Meals within a day are ordered by
// canonicalMealOrder, with unrecognized groups appended in first-seen order.
func BuildDayDetails(days []DayTotals, servings []ServingEntry, exercises []ExerciseEntry, biometrics []BiometricEntry, notes []NoteEntry) []DayDetail {
	index := make(map[time.Time]*DayDetail)
	var order []time.Time
	get := func(d time.Time) *DayDetail {
		if dd, ok := index[d]; ok {
			return dd
		}
		dd := &DayDetail{Date: d}
		index[d] = dd
		order = append(order, d)
		return dd
	}

	for _, dt := range days {
		get(dt.Date).Totals = dt.Values
	}

	// Group servings by day then meal, preserving first-seen meal order per day.
	mealIndex := make(map[time.Time]map[string]*MealDetail)
	mealOrder := make(map[time.Time][]string)
	for _, s := range servings {
		get(s.Date) // ensure the day exists in order
		mi := mealIndex[s.Date]
		if mi == nil {
			mi = make(map[string]*MealDetail)
			mealIndex[s.Date] = mi
		}
		md, ok := mi[s.Meal]
		if !ok {
			md = &MealDetail{Name: s.Meal}
			mi[s.Meal] = md
			mealOrder[s.Date] = append(mealOrder[s.Date], s.Meal)
		}
		md.Foods = append(md.Foods, s)
	}
	for d, names := range mealOrder {
		sort.SliceStable(names, func(i, j int) bool { return mealRank(names[i]) < mealRank(names[j]) })
		dd := index[d]
		for _, name := range names {
			dd.Meals = append(dd.Meals, *mealIndex[d][name])
		}
	}

	for _, e := range exercises {
		dd := get(e.Date)
		dd.Exercises = append(dd.Exercises, e)
	}

	// Aggregate biometrics per (day, metric) so high-frequency samples collapse to one row.
	bioIndex := make(map[time.Time]map[string]*bioAccum)
	bioOrder := make(map[time.Time][]string)
	for _, b := range biometrics {
		get(b.Date) // ensure the day exists in order
		bi := bioIndex[b.Date]
		if bi == nil {
			bi = make(map[string]*bioAccum)
			bioIndex[b.Date] = bi
		}
		acc, ok := bi[b.Metric]
		if !ok {
			acc = &bioAccum{metric: b.Metric, unit: b.Unit, min: b.Value, max: b.Value}
			bi[b.Metric] = acc
			bioOrder[b.Date] = append(bioOrder[b.Date], b.Metric)
		}
		acc.add(b)
	}
	for d, metrics := range bioOrder {
		dd := index[d]
		for _, m := range metrics {
			dd.Biometrics = append(dd.Biometrics, bioIndex[d][m].summary())
		}
	}

	for _, n := range notes {
		dd := get(n.Date)
		dd.Notes = append(dd.Notes, n)
	}

	sort.Slice(order, func(i, j int) bool { return order[i].Before(order[j]) })
	out := make([]DayDetail, 0, len(order))
	for _, d := range order {
		out = append(out, *index[d])
	}
	return out
}

// bioAccum accumulates same-metric measurements to produce a BiometricSummary.
type bioAccum struct {
	metric string
	unit   string
	count  int
	sum    float64
	min    float64
	max    float64
	last   float64
}

func (a *bioAccum) add(b BiometricEntry) {
	a.count++
	a.sum += b.Value
	a.last = b.Value
	if b.Value < a.min {
		a.min = b.Value
	}
	if b.Value > a.max {
		a.max = b.Value
	}
}

func (a *bioAccum) summary() BiometricSummary {
	avg := a.sum
	if a.count > 0 {
		avg = a.sum / float64(a.count)
	}
	return BiometricSummary{
		Metric: a.metric,
		Unit:   a.unit,
		Count:  a.count,
		Min:    a.min,
		Max:    a.max,
		Avg:    avg,
		Last:   a.last,
	}
}
