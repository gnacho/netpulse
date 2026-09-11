// alerts_i18n_test.go — contrato de slugs de alertas con sus claves i18n (#712).
package alerts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// newTypeSlugs: slugs añadidos en #712 para los emisores que seguían sin
// Type/Vars (recuperaciones, beacon, rearme, SFP, WireGuard...).
var newTypeSlugs = []string{
	TypeAgentRecovered, TypeSSHAccessLost, TypeSSHAccessRecovered,
	TypeAgentUpdated, TypeRouterRecovered, TypeWireguardHandshake,
	TypePortStable, TypeGhostPortRecovered, TypeLinkRecovered,
	TypeSwitchLoop, TypePortDisabled, TypePortRecovered,
	TypeLinkDown, TypeLinkUp, TypeSfpRxLow, TypeSfpTempHigh,
	TypeSwitchRebooted, TypeAgentRearmed, TypeAgentReinstalled,
	TypeAutoRearmFailed,
}

// hintSlugs: slugs ya existentes en #310/#671 (con hint) para detectar colisiones.
var hintSlugs = []string{
	HintAgentDown, HintAgentDownSSH, HintGatewayUnrch, HintHighTemp,
	HintPortFlapping, HintDeviceOffline, HintUnknownDevice, HintFirmware,
	HintWanDown, HintWifiWeak, HintGhostPort, HintDegradedLink,
	HintAgentOutdated, HintWanSlow,
}

var kebabRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// TestNewTypeSlugsAreKebabCaseAndUnique: los slugs nuevos son kebab-case y no
// colisionan con los existentes.
func TestNewTypeSlugsAreKebabCaseAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, h := range hintSlugs {
		seen[h] = true
	}
	for _, slug := range newTypeSlugs {
		if !kebabRe.MatchString(slug) {
			t.Errorf("slug %q no es kebab-case", slug)
		}
		if seen[slug] {
			t.Errorf("slug %q colisiona con un hint/slug existente", slug)
		}
		seen[slug] = true
	}
	if len(newTypeSlugs) != 20 {
		t.Errorf("se esperan 20 slugs nuevos, hay %d", len(newTypeSlugs))
	}
}

// TestAlertTypeSlugsHaveI18nKeys: cada slug nuevo tiene title y description
// en ES y EN. Lee los catálogos de la app (contrato server <-> frontend).
func TestAlertTypeSlugsHaveI18nKeys(t *testing.T) {
	root := filepath.Join("..", "..", "..") // server-go/internal/alerts -> repo root
	for _, lang := range []string{"es", "en"} {
		path := filepath.Join(root, "app", "public", "locales", lang, "translation.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("no se pudo leer %s: %v", path, err)
		}
		var tree map[string]interface{}
		if err := json.Unmarshal(data, &tree); err != nil {
			t.Fatalf("json inválido en %s: %v", path, err)
		}
		alertsNode, ok := tree["alerts"].(map[string]interface{})
		if !ok {
			t.Fatalf("%s: falta alerts", lang)
		}
		types, ok := alertsNode["types"].(map[string]interface{})
		if !ok {
			t.Fatalf("%s: falta alerts.types", lang)
		}
		for _, slug := range newTypeSlugs {
			entry, ok := types[slug].(map[string]interface{})
			if !ok {
				t.Errorf("%s: falta alerts.types.%s", lang, slug)
				continue
			}
			for _, key := range []string{"title", "description"} {
				if v, ok := entry[key].(string); !ok || strings.TrimSpace(v) == "" {
					t.Errorf("%s: alerts.types.%s.%s vacío o ausente", lang, slug, key)
				}
			}
		}
	}
}
