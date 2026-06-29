package cronometer

import (
	"fmt"
	"strings"
)

// Nutrient is one selectable column in the daily-nutrition summary.
type Nutrient struct {
	Key     string
	Column  string
	Unit    string
	Aliases []string
	Default bool
}

// Registry is the canonical, display-ordered list of nutrients Cronometer reports. The column
// names mirror Cronometer's export headers; the parser matches against them. Cronometer may add
// nutrients over time — unknown columns are simply ignored by the parser.
var Registry = []Nutrient{
	// Macros (shown by default).
	{Key: "energy", Column: "Energy (kcal)", Unit: "kcal", Default: true, Aliases: []string{"calories", "cals", "kcal"}},
	{Key: "protein", Column: "Protein (g)", Unit: "g", Default: true},
	{Key: "carbs", Column: "Carbs (g)", Unit: "g", Default: true, Aliases: []string{"carbohydrates", "carbohydrate", "carb"}},
	{Key: "fat", Column: "Fat (g)", Unit: "g", Default: true},

	// Carbohydrate breakdown.
	{Key: "fiber", Column: "Fiber (g)", Unit: "g"},
	{Key: "net-carbs", Column: "Net Carbs (g)", Unit: "g", Aliases: []string{"netcarbs"}},
	{Key: "sugars", Column: "Sugars (g)", Unit: "g", Aliases: []string{"sugar"}},
	{Key: "starch", Column: "Starch (g)", Unit: "g"},
	{Key: "fructose", Column: "Fructose (g)", Unit: "g"},
	{Key: "galactose", Column: "Galactose (g)", Unit: "g"},
	{Key: "glucose", Column: "Glucose (g)", Unit: "g"},
	{Key: "lactose", Column: "Lactose (g)", Unit: "g"},
	{Key: "maltose", Column: "Maltose (g)", Unit: "g"},
	{Key: "sucrose", Column: "Sucrose (g)", Unit: "g"},

	// Fat breakdown.
	{Key: "saturated", Column: "Saturated (g)", Unit: "g", Aliases: []string{"sat-fat", "saturated-fat"}},
	{Key: "monounsaturated", Column: "Monounsaturated (g)", Unit: "g", Aliases: []string{"mono-fat", "monounsaturated-fat"}},
	{Key: "polyunsaturated", Column: "Polyunsaturated (g)", Unit: "g", Aliases: []string{"poly-fat", "polyunsaturated-fat"}},
	{Key: "trans-fat", Column: "Trans-Fats (g)", Unit: "g", Aliases: []string{"trans", "trans-fats"}},
	{Key: "omega-3", Column: "Omega-3 (g)", Unit: "g", Aliases: []string{"omega3"}},
	{Key: "omega-6", Column: "Omega-6 (g)", Unit: "g", Aliases: []string{"omega6"}},
	{Key: "cholesterol", Column: "Cholesterol (mg)", Unit: "mg"},

	// Vitamins.
	{Key: "vitamin-a", Column: "Vitamin A (µg)", Unit: "µg", Aliases: []string{"vit-a"}},
	{Key: "b1", Column: "B1 (Thiamine) (mg)", Unit: "mg", Aliases: []string{"thiamine", "vitamin-b1"}},
	{Key: "b2", Column: "B2 (Riboflavin) (mg)", Unit: "mg", Aliases: []string{"riboflavin", "vitamin-b2"}},
	{Key: "b3", Column: "B3 (Niacin) (mg)", Unit: "mg", Aliases: []string{"niacin", "vitamin-b3"}},
	{Key: "b5", Column: "B5 (Pantothenic Acid) (mg)", Unit: "mg", Aliases: []string{"pantothenic-acid", "vitamin-b5"}},
	{Key: "b6", Column: "B6 (Pyridoxine) (mg)", Unit: "mg", Aliases: []string{"pyridoxine", "vitamin-b6"}},
	{Key: "b12", Column: "B12 (Cobalamin) (µg)", Unit: "µg", Aliases: []string{"cobalamin", "vitamin-b12"}},
	{Key: "vitamin-c", Column: "Vitamin C (mg)", Unit: "mg", Aliases: []string{"vit-c"}},
	{Key: "vitamin-d", Column: "Vitamin D (IU)", Unit: "IU", Aliases: []string{"vit-d"}},
	{Key: "vitamin-e", Column: "Vitamin E (mg)", Unit: "mg", Aliases: []string{"vit-e"}},
	{Key: "vitamin-k", Column: "Vitamin K (µg)", Unit: "µg", Aliases: []string{"vit-k"}},
	{Key: "biotin", Column: "Biotin (µg)", Unit: "µg"},
	{Key: "choline", Column: "Choline (mg)", Unit: "mg"},
	{Key: "folate", Column: "Folate (µg)", Unit: "µg"},

	// Minerals.
	{Key: "calcium", Column: "Calcium (mg)", Unit: "mg"},
	{Key: "chromium", Column: "Chromium (µg)", Unit: "µg"},
	{Key: "copper", Column: "Copper (mg)", Unit: "mg"},
	{Key: "fluoride", Column: "Fluoride (µg)", Unit: "µg"},
	{Key: "iodine", Column: "Iodine (µg)", Unit: "µg"},
	{Key: "iron", Column: "Iron (mg)", Unit: "mg"},
	{Key: "magnesium", Column: "Magnesium (mg)", Unit: "mg"},
	{Key: "manganese", Column: "Manganese (mg)", Unit: "mg"},
	{Key: "phosphorus", Column: "Phosphorus (mg)", Unit: "mg"},
	{Key: "potassium", Column: "Potassium (mg)", Unit: "mg"},
	{Key: "selenium", Column: "Selenium (µg)", Unit: "µg"},
	{Key: "sodium", Column: "Sodium (mg)", Unit: "mg"},
	{Key: "zinc", Column: "Zinc (mg)", Unit: "mg"},

	// Amino acids.
	{Key: "cystine", Column: "Cystine (g)", Unit: "g"},
	{Key: "histidine", Column: "Histidine (g)", Unit: "g"},
	{Key: "isoleucine", Column: "Isoleucine (g)", Unit: "g"},
	{Key: "leucine", Column: "Leucine (g)", Unit: "g"},
	{Key: "lysine", Column: "Lysine (g)", Unit: "g"},
	{Key: "methionine", Column: "Methionine (g)", Unit: "g"},
	{Key: "phenylalanine", Column: "Phenylalanine (g)", Unit: "g"},
	{Key: "threonine", Column: "Threonine (g)", Unit: "g"},
	{Key: "tryptophan", Column: "Tryptophan (g)", Unit: "g"},
	{Key: "tyrosine", Column: "Tyrosine (g)", Unit: "g"},
	{Key: "valine", Column: "Valine (g)", Unit: "g"},

	// Other.
	{Key: "water", Column: "Water (g)", Unit: "g"},
	{Key: "caffeine", Column: "Caffeine (mg)", Unit: "mg"},
	{Key: "alcohol", Column: "Alcohol (g)", Unit: "g"},
}

// nutrientIndex maps every canonical key and alias (normalized) to its Nutrient.
var nutrientIndex = buildNutrientIndex()

func buildNutrientIndex() map[string]Nutrient {
	idx := make(map[string]Nutrient, len(Registry)*2)
	for _, n := range Registry {
		idx[normalizeNutrientName(n.Key)] = n
		for _, a := range n.Aliases {
			idx[normalizeNutrientName(a)] = n
		}
	}
	return idx
}

// normalizeNutrientName lowercases and collapses spaces/underscores to hyphens so "Vitamin C",
// "vitamin_c", and "vitamin-c" all resolve alike.
func normalizeNutrientName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "_", "-")
	return name
}

// ResolveNutrient looks up a single nutrient by canonical key or alias, case-insensitively.
func ResolveNutrient(name string) (Nutrient, bool) {
	n, ok := nutrientIndex[normalizeNutrientName(name)]
	return n, ok
}

// ResolveList resolves a comma-separated list of nutrient names into Nutrients, preserving order
// and dropping duplicates. It errors on the first unrecognized name.
func ResolveList(list string) ([]Nutrient, error) {
	var out []Nutrient
	seen := make(map[string]struct{})
	for raw := range strings.SplitSeq(list, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		n, ok := ResolveNutrient(name)
		if !ok {
			return nil, fmt.Errorf("unknown nutrient %q (run \"crono summary --list-nutrients\" to see all)", name)
		}
		if _, dup := seen[n.Key]; dup {
			continue
		}
		seen[n.Key] = struct{}{}
		out = append(out, n)
	}
	return out, nil
}

// DefaultNutrients returns the nutrients shown when no selection flags are provided.
func DefaultNutrients() []Nutrient {
	var out []Nutrient
	for _, n := range Registry {
		if n.Default {
			out = append(out, n)
		}
	}
	return out
}
