package cronometer

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
)

// errWriter wraps an io.Writer and records the first write error, so a run of formatted writes
// can be made without checking each call; inspect err once at the end (Rob Pike, "Errors are
// values").
type errWriter struct {
	w   io.Writer
	err error
}

func (ew *errWriter) printf(format string, a ...any) {
	if ew.err != nil {
		return
	}
	_, ew.err = fmt.Fprintf(ew.w, format, a...)
}

// Format is an output format for a summary.
type Format string

// Supported output formats.
const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatCSV   Format = "csv"
)

// ParseFormat validates and returns a Format.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatTable, FormatJSON, FormatCSV:
		return Format(s), nil
	default:
		return "", fmt.Errorf("unknown format %q (want table, json, or csv)", s)
	}
}

// Render writes the days/nutrients selection to w in the requested format. When averages is
// true, table and csv output gain a trailing average row.
func Render(w io.Writer, format Format, days []DayTotals, nutrients []Nutrient, averages bool) error {
	switch format {
	case FormatJSON:
		return renderJSON(w, days, nutrients)
	case FormatCSV:
		return renderCSV(w, days, nutrients, averages)
	case FormatTable:
		return renderTable(w, days, nutrients, averages)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}

// renderTable writes a human-readable table. A single day is rendered as a vertical list;
// multiple days as a date-by-nutrient grid.
func renderTable(w io.Writer, days []DayTotals, nutrients []Nutrient, averages bool) error {
	if len(days) == 0 {
		_, err := fmt.Fprintln(w, "No data for the requested range.")
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	ew := &errWriter{w: tw}

	if len(days) == 1 {
		day := days[0]
		ew.printf("%s\n", day.Date.Format(DateLayout))
		for _, n := range nutrients {
			ew.printf("  %s\t%s %s\n", n.Key, formatValue(day.Values[n.Key]), n.Unit)
		}
		return flush(tw, ew)
	}

	// Header row.
	ew.printf("Date")
	for _, n := range nutrients {
		ew.printf("\t%s (%s)", n.Key, n.Unit)
	}
	ew.printf("\n")

	for _, day := range days {
		ew.printf("%s", day.Date.Format(DateLayout))
		for _, n := range nutrients {
			ew.printf("\t%s", formatValue(day.Values[n.Key]))
		}
		ew.printf("\n")
	}

	if averages {
		avg := averageValues(days, nutrients)
		ew.printf("Average")
		for _, n := range nutrients {
			ew.printf("\t%s", formatValue(avg[n.Key]))
		}
		ew.printf("\n")
	}

	return flush(tw, ew)
}

// flush flushes the tabwriter and returns the first of any write error or flush error.
func flush(tw *tabwriter.Writer, ew *errWriter) error {
	if ferr := tw.Flush(); ferr != nil && ew.err == nil {
		return fmt.Errorf("flushing output: %w", ferr)
	}
	return ew.err
}

// RenderNutrientList writes the full nutrient registry (name, unit, default status, aliases) as
// a table to w.
func RenderNutrientList(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	ew := &errWriter{w: tw}

	ew.printf("NAME\tUNIT\tDEFAULT\tALIASES\n")
	for _, n := range Registry {
		def := ""
		if n.Default {
			def = "yes"
		}
		ew.printf("%s\t%s\t%s\t%s\n", n.Key, n.Unit, def, strings.Join(n.Aliases, ", "))
	}

	return flush(tw, ew)
}

// dayJSON is the JSON shape for one day.
type dayJSON struct {
	Nutrients map[string]nutrientVal `json:"nutrients"`
	Date      string                 `json:"date"`
}

type nutrientVal struct {
	Unit  string  `json:"unit"`
	Value float64 `json:"value"`
}

// renderJSON writes an array of day objects, each carrying the selected nutrients.
func renderJSON(w io.Writer, days []DayTotals, nutrients []Nutrient) error {
	out := make([]dayJSON, 0, len(days))
	for _, day := range days {
		nv := make(map[string]nutrientVal, len(nutrients))
		for _, n := range nutrients {
			nv[n.Key] = nutrientVal{Value: day.Values[n.Key], Unit: n.Unit}
		}
		out = append(out, dayJSON{Date: day.Date.Format(DateLayout), Nutrients: nv})
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}
	return nil
}

// renderCSV writes a Date + selected-nutrient-columns CSV (a filtered re-export).
func renderCSV(w io.Writer, days []DayTotals, nutrients []Nutrient, averages bool) error {
	cw := csv.NewWriter(w)

	header := make([]string, 0, len(nutrients)+1)
	header = append(header, "Date")
	for _, n := range nutrients {
		header = append(header, n.Column)
	}
	if err := cw.Write(header); err != nil {
		return fmt.Errorf("writing CSV header: %w", err)
	}

	for _, day := range days {
		row := make([]string, 0, len(nutrients)+1)
		row = append(row, day.Date.Format(DateLayout))
		for _, n := range nutrients {
			row = append(row, strconv.FormatFloat(day.Values[n.Key], 'f', -1, 64))
		}
		if err := cw.Write(row); err != nil {
			return fmt.Errorf("writing CSV row: %w", err)
		}
	}

	if averages {
		avg := averageValues(days, nutrients)
		row := make([]string, 0, len(nutrients)+1)
		row = append(row, "Average")
		for _, n := range nutrients {
			row = append(row, strconv.FormatFloat(avg[n.Key], 'f', -1, 64))
		}
		if err := cw.Write(row); err != nil {
			return fmt.Errorf("writing CSV average row: %w", err)
		}
	}

	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("flushing CSV: %w", err)
	}
	return nil
}

// averageValues returns the per-nutrient mean across days.
func averageValues(days []DayTotals, nutrients []Nutrient) map[string]float64 {
	avg := make(map[string]float64, len(nutrients))
	if len(days) == 0 {
		return avg
	}
	for _, n := range nutrients {
		var sum float64
		for _, day := range days {
			sum += day.Values[n.Key]
		}
		avg[n.Key] = sum / float64(len(days))
	}
	return avg
}

// formatValue renders a nutrient amount to one decimal place.
func formatValue(v float64) string {
	return strconv.FormatFloat(v, 'f', 1, 64)
}
