// icon_catalog_test.go — the icon catalog is written in four places that have
// to agree: ICON_OVERRIDES and ICON_CATEGORIES in DeviceRow.tsx, validateIcon
// in this package, and devices.edit.iconNames in the four locale catalogs.
// The app has no JS tests (pattern of alerts/frontend_defaults_test.go), so the
// parity is asserted here over the source files. Without it an icon added to
// one place and forgotten in another reaches the user as a tile with no name,
// a name with no tile, or a selectable icon that no category ever renders.
package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

const iconCatalogSrc = "../../../app/src/components/DeviceRow.tsx"

// hiddenIcon is in ICON_OVERRIDES but IconPicker keeps it out of the grid.
const hiddenIcon = "help-circle"

var (
	overrideKeyRe  = regexp.MustCompile(`(?m)^\s*(?:'([a-z0-9-]+)'|([a-z0-9-]+)):\s*[A-Z]`)
	categoryRe     = regexp.MustCompile(`id:\s*'([a-z-]+)'[\s\S]*?icons:\s*\[([^\]]*)\]`)
	categoryIconRe = regexp.MustCompile(`'([a-z0-9-]+)'`)
	whitelistKeyRe = regexp.MustCompile(`"([a-z0-9-]+)":\s*true`)
)

func readCatalogSource(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(raw)
}

// catalogBlock cuts the source between two declarations, so a key added to a
// neighbouring map cannot be counted as part of the catalog.
func catalogBlock(t *testing.T, src, from, to string) string {
	t.Helper()
	start := strings.Index(src, from)
	if start < 0 {
		t.Fatalf("%s: %s not found", iconCatalogSrc, from)
	}
	rest := src[start:]
	end := strings.Index(rest, to)
	if end < 0 {
		t.Fatalf("%s: %s not found after %s", iconCatalogSrc, to, from)
	}
	return rest[:end]
}

func iconOverrideKeys(t *testing.T) []string {
	t.Helper()
	src := readCatalogSource(t, iconCatalogSrc)
	block := catalogBlock(t, src, "export const ICON_OVERRIDES", "export const ALLOWED_ICONS")
	var keys []string
	seen := map[string]bool{}
	for _, m := range overrideKeyRe.FindAllStringSubmatch(block, -1) {
		key := m[1]
		if key == "" {
			key = m[2]
		}
		if seen[key] {
			t.Errorf("ICON_OVERRIDES declares %q twice", key)
		}
		seen[key] = true
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		t.Fatalf("%s: no ICON_OVERRIDES keys parsed", iconCatalogSrc)
	}
	return keys
}

func iconCategoryIDs(t *testing.T) []string {
	t.Helper()
	src := readCatalogSource(t, iconCatalogSrc)
	block := catalogBlock(t, src, "export const ICON_CATEGORIES", "export function deviceIcon")
	matches := categoryRe.FindAllStringSubmatch(block, -1)
	if len(matches) == 0 {
		t.Fatalf("%s: no ICON_CATEGORIES entries parsed", iconCatalogSrc)
	}
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m[1])
	}
	return ids
}

// TestIconCategoriesCoverEverySelectableIcon: every icon the picker offers sits
// in exactly one category, and no category lists an icon the picker cannot
// render. An icon in two categories renders twice; an icon in none is invisible
// outside search.
func TestIconCategoriesCoverEverySelectableIcon(t *testing.T) {
	overrides := iconOverrideKeys(t)
	selectable := map[string]bool{}
	for _, key := range overrides {
		if key != hiddenIcon {
			selectable[key] = true
		}
	}

	src := readCatalogSource(t, iconCatalogSrc)
	block := catalogBlock(t, src, "export const ICON_CATEGORIES", "export function deviceIcon")
	count := map[string]int{}
	seenID := map[string]bool{}
	for _, m := range categoryRe.FindAllStringSubmatch(block, -1) {
		id := m[1]
		if seenID[id] {
			t.Errorf("category id %q is declared twice", id)
		}
		seenID[id] = true
		for _, im := range categoryIconRe.FindAllStringSubmatch(m[2], -1) {
			icon := im[1]
			count[icon]++
			if !selectable[icon] {
				t.Errorf("category %q lists %q, which ICON_OVERRIDES does not offer", id, icon)
			}
		}
	}

	var missing []string
	for icon := range selectable {
		switch count[icon] {
		case 0:
			missing = append(missing, icon)
		case 1:
		default:
			t.Errorf("icon %q sits in %d categories, want exactly 1", icon, count[icon])
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("selectable icons in no category, so the grid never shows them: %s", strings.Join(missing, ", "))
	}
}

// TestIconWhitelistMatchesOverrides: the closed server list and the frontend
// catalog are the same set in both directions, and validateIcon really is the
// function enforcing it.
func TestIconWhitelistMatchesOverrides(t *testing.T) {
	overrides := iconOverrideKeys(t)
	want := map[string]bool{}
	for _, key := range overrides {
		want[key] = true
		if !validateIcon(key) {
			t.Errorf("validateIcon rejects %q, which ICON_OVERRIDES offers", key)
		}
	}
	if validateIcon("zzz-not-an-icon") {
		t.Error("validateIcon accepts a key outside the catalog: the list is not closed")
	}

	src := readCatalogSource(t, "device_overrides.go")
	start := strings.Index(src, "allowed := map[string]bool{")
	if start < 0 {
		t.Fatal("device_overrides.go: the validateIcon whitelist was not found")
	}
	rest := src[start:]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		t.Fatal("device_overrides.go: the validateIcon whitelist is not closed")
	}
	got := map[string]bool{}
	for _, m := range whitelistKeyRe.FindAllStringSubmatch(rest[:end], -1) {
		got[m[1]] = true
	}
	if len(got) == 0 {
		t.Fatal("device_overrides.go: no whitelist keys parsed")
	}
	for key := range got {
		if !want[key] {
			t.Errorf("validateIcon allows %q with no ICON_OVERRIDES entry", key)
		}
	}
	for key := range want {
		if !got[key] {
			t.Errorf("ICON_OVERRIDES offers %q but the validateIcon whitelist does not allow it", key)
		}
	}
}

// TestIconLocaleCatalogsMatchOverrides: every icon has a non-empty name and
// every category a non-empty label in en, es, zh and ru, with no orphan keys on
// either side. A missing name renders the raw key in the picker.
func TestIconLocaleCatalogsMatchOverrides(t *testing.T) {
	overrides := iconOverrideKeys(t)
	want := map[string]bool{}
	for _, key := range overrides {
		want[key] = true
	}
	ids := iconCategoryIDs(t)

	for _, lang := range []string{"en", "es", "zh", "ru"} {
		rel := filepath.Join("..", "..", "..", "app", "public", "locales", lang, "translation.json")
		raw, err := os.ReadFile(rel)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		var tree map[string]any
		if err := json.Unmarshal(raw, &tree); err != nil {
			t.Fatalf("%s: invalid JSON: %v", rel, err)
		}
		edit := catalogNode(t, tree, lang, "devices", "edit")
		names := catalogNode(t, edit, lang, "iconNames")
		for key := range want {
			value, ok := names[key]
			if !ok {
				t.Errorf("%s: iconNames has no entry for %q", lang, key)
				continue
			}
			if name, ok := value.(string); !ok || strings.TrimSpace(name) == "" {
				t.Errorf("%s: iconNames[%q] is not a non-empty string", lang, key)
			}
		}
		for key := range names {
			if !want[key] {
				t.Errorf("%s: iconNames[%q] is an orphan, ICON_OVERRIDES has no such icon", lang, key)
			}
		}

		labels := catalogNode(t, edit, lang, "iconCategories")
		for _, id := range ids {
			value, ok := labels[id]
			if !ok {
				t.Errorf("%s: iconCategories has no label for category %q", lang, id)
				continue
			}
			if label, ok := value.(string); !ok || strings.TrimSpace(label) == "" {
				t.Errorf("%s: iconCategories[%q] is not a non-empty string", lang, id)
			}
		}
		for id := range labels {
			if !slices.Contains(ids, id) {
				t.Errorf("%s: iconCategories[%q] is an orphan, no such category", lang, id)
			}
		}
	}
}

func catalogNode(t *testing.T, tree map[string]any, lang string, path ...string) map[string]any {
	t.Helper()
	current := tree
	for _, key := range path {
		next, ok := current[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s is missing or not an object", lang, key)
		}
		current = next
	}
	return current
}
