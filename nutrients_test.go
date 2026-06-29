package cronometer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveNutrient(T *testing.T) {
	T.Parallel()

	T.Run("canonical key", func(t *testing.T) {
		t.Parallel()
		n, ok := ResolveNutrient("protein")
		require.True(t, ok)
		assert.Equal(t, "protein", n.Key)
		assert.Equal(t, "Protein (g)", n.Column)
	})

	T.Run("alias resolves to canonical", func(t *testing.T) {
		t.Parallel()
		n, ok := ResolveNutrient("calories")
		require.True(t, ok)
		assert.Equal(t, "energy", n.Key)
	})

	T.Run("case and separator insensitive", func(t *testing.T) {
		t.Parallel()
		for _, name := range []string{"Vitamin C", "vitamin_c", "VITAMIN-C", "vit-c"} {
			n, ok := ResolveNutrient(name)
			require.Truef(t, ok, "expected %q to resolve", name)
			assert.Equalf(t, "vitamin-c", n.Key, "for %q", name)
		}
	})

	T.Run("unknown name", func(t *testing.T) {
		t.Parallel()
		_, ok := ResolveNutrient("unobtainium")
		assert.False(t, ok)
	})
}

func TestResolveList(T *testing.T) {
	T.Parallel()

	T.Run("resolves and dedupes preserving order", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveList("fiber, caffeine, fiber, protein")
		require.NoError(t, err)
		keys := nutrientKeys(got)
		assert.Equal(t, []string{"fiber", "caffeine", "protein"}, keys)
	})

	T.Run("alias and canonical collapse to one", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveList("carbs,carbohydrates")
		require.NoError(t, err)
		assert.Equal(t, []string{"carbs"}, nutrientKeys(got))
	})

	T.Run("errors on unknown member", func(t *testing.T) {
		t.Parallel()
		_, err := ResolveList("protein,bogus")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bogus")
	})

	T.Run("ignores empty members", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveList("protein,, ,fat")
		require.NoError(t, err)
		assert.Equal(t, []string{"protein", "fat"}, nutrientKeys(got))
	})
}

func TestDefaultNutrients(T *testing.T) {
	T.Parallel()

	T.Run("macros are the defaults", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"energy", "protein", "carbs", "fat"}, nutrientKeys(DefaultNutrients()))
	})
}

func TestRegistryIntegrity(T *testing.T) {
	T.Parallel()

	T.Run("keys and columns are unique", func(t *testing.T) {
		t.Parallel()
		keys := map[string]struct{}{}
		cols := map[string]struct{}{}
		for _, n := range Registry {
			_, dupKey := keys[n.Key]
			assert.Falsef(t, dupKey, "duplicate key %q", n.Key)
			keys[n.Key] = struct{}{}

			_, dupCol := cols[n.Column]
			assert.Falsef(t, dupCol, "duplicate column %q", n.Column)
			cols[n.Column] = struct{}{}
		}
	})

	T.Run("aliases do not collide with keys or other aliases", func(t *testing.T) {
		t.Parallel()
		// buildNutrientIndex would silently overwrite on collision; assert the index size matches
		// the distinct names so nothing is shadowed.
		distinct := map[string]struct{}{}
		for _, n := range Registry {
			distinct[normalizeNutrientName(n.Key)] = struct{}{}
			for _, a := range n.Aliases {
				distinct[normalizeNutrientName(a)] = struct{}{}
			}
		}
		assert.Len(t, nutrientIndex, len(distinct))
	})
}

func nutrientKeys(ns []Nutrient) []string {
	keys := make([]string, len(ns))
	for i, n := range ns {
		keys[i] = n.Key
	}
	return keys
}
