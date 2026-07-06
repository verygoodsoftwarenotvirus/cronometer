package cronometer

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
)

// RenderDetailed writes a per-day, per-meal breakdown (plus activity, biomarkers, notes, and the
// daily nutrient total) to w. Only table and JSON are supported; the daily total honors the
// selected nutrients, while food and meal-subtotal lines always show macros.
func RenderDetailed(w io.Writer, format Format, details []DayDetail, nutrients []Nutrient) error {
	switch format {
	case FormatJSON:
		return renderDetailedJSON(w, details, nutrients)
	case FormatTable:
		return renderDetailedTable(w, details, nutrients)
	case FormatCSV:
		return fmt.Errorf("detailed output supports only table or json")
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}

func renderDetailedTable(w io.Writer, details []DayDetail, nutrients []Nutrient) error {
	if len(details) == 0 {
		_, err := fmt.Fprintln(w, "No data for the requested range.")
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	ew := &errWriter{w: tw}

	for i, day := range details {
		if i > 0 {
			ew.printf("\n")
		}
		ew.printf("%s\n", day.Date.Format(DateLayout))

		if len(day.Meals) == 0 {
			ew.printf("  (no food logged)\n")
		}
		for _, meal := range day.Meals {
			ew.printf("  %s\n", meal.Name)
			for _, food := range meal.Foods {
				ew.printf("    %s\t%s\t%s\t%s\n", food.Time, food.Food, food.Amount, food.Category)
			}
		}

		if len(day.Exercises) > 0 {
			ew.printf("  Activity\n")
			for _, ex := range day.Exercises {
				ew.printf("    %s\t%s min\t%s kcal\n", ex.Name, formatValue(ex.Minutes), formatValue(ex.Calories))
			}
		}

		if len(day.Biometrics) > 0 {
			ew.printf("  Biomarkers\n")
			for _, b := range day.Biometrics {
				ew.printf("    %s\t%s\n", b.Metric, biometricCells(b))
			}
		}

		if len(day.Notes) > 0 {
			ew.printf("  Notes\n")
			for _, n := range day.Notes {
				ew.printf("    %s\n", n.Text)
			}
		}

		ew.printf("  Daily total\n")
		for _, n := range nutrients {
			ew.printf("    %s\t%s %s\n", n.Key, formatValue(day.Totals[n.Key]), n.Unit)
		}
	}

	return flush(tw, ew)
}

// biometricCells formats a biometric summary: a single reading shows just its value, while multiple
// samples show min/avg/max plus the sample count.
func biometricCells(b BiometricSummary) string {
	if b.Count <= 1 {
		return fmt.Sprintf("%s %s", formatValue(b.Last), b.Unit)
	}
	return fmt.Sprintf("min %s / avg %s / max %s %s\t(%d samples)",
		formatValue(b.Min), formatValue(b.Avg), formatValue(b.Max), b.Unit, b.Count)
}

// JSON shapes for the detailed view.
type detailJSON struct {
	Date       string                 `json:"date"`
	Totals     map[string]nutrientVal `json:"totals"`
	Meals      []mealJSON             `json:"meals"`
	Exercises  []exerciseJSON         `json:"exercises"`
	Biometrics []biometricJSON        `json:"biometrics"`
	Notes      []string               `json:"notes"`
}

type mealJSON struct {
	Name  string     `json:"name"`
	Foods []foodJSON `json:"foods"`
}

type foodJSON struct {
	Time     string `json:"time"`
	Food     string `json:"food"`
	Amount   string `json:"amount"`
	Category string `json:"category"`
}

type exerciseJSON struct {
	Name     string  `json:"name"`
	Minutes  float64 `json:"minutes"`
	Calories float64 `json:"calories"`
}

type biometricJSON struct {
	Metric string  `json:"metric"`
	Unit   string  `json:"unit"`
	Count  int     `json:"count"`
	Min    float64 `json:"min"`
	Avg    float64 `json:"avg"`
	Max    float64 `json:"max"`
	Last   float64 `json:"last"`
}

// totalsVals builds a nutrientVal map for the selected nutrients from a totals map.
func totalsVals(values map[string]float64, nutrients []Nutrient) map[string]nutrientVal {
	out := make(map[string]nutrientVal, len(nutrients))
	for _, n := range nutrients {
		out[n.Key] = nutrientVal{Value: values[n.Key], Unit: n.Unit}
	}
	return out
}

func renderDetailedJSON(w io.Writer, details []DayDetail, nutrients []Nutrient) error {
	out := make([]detailJSON, 0, len(details))
	for _, day := range details {
		meals := make([]mealJSON, 0, len(day.Meals))
		for _, meal := range day.Meals {
			foods := make([]foodJSON, 0, len(meal.Foods))
			for _, food := range meal.Foods {
				foods = append(foods, foodJSON{
					Time:     food.Time,
					Food:     food.Food,
					Amount:   food.Amount,
					Category: food.Category,
				})
			}
			meals = append(meals, mealJSON{
				Name:  meal.Name,
				Foods: foods,
			})
		}

		exercises := make([]exerciseJSON, 0, len(day.Exercises))
		for _, ex := range day.Exercises {
			exercises = append(exercises, exerciseJSON{Name: ex.Name, Minutes: ex.Minutes, Calories: ex.Calories})
		}

		biometrics := make([]biometricJSON, 0, len(day.Biometrics))
		for _, b := range day.Biometrics {
			biometrics = append(biometrics, biometricJSON{
				Metric: b.Metric,
				Unit:   b.Unit,
				Count:  b.Count,
				Min:    b.Min,
				Avg:    b.Avg,
				Max:    b.Max,
				Last:   b.Last,
			})
		}

		notes := make([]string, 0, len(day.Notes))
		for _, n := range day.Notes {
			notes = append(notes, n.Text)
		}

		out = append(out, detailJSON{
			Date:       day.Date.Format(DateLayout),
			Totals:     totalsVals(day.Totals, nutrients),
			Meals:      meals,
			Exercises:  exercises,
			Biometrics: biometrics,
			Notes:      notes,
		})
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}
	return nil
}
