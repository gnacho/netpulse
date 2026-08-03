package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Test de frescura del canon (SPEC-65 D65-1): app/src/data/demo-canon.json es
// un artefacto GENERADO desde el canon Go — el test exige igualdad profunda
// con BuildDemoCanon (tras normalizar ambos lados vía JSON marshal/unmarshal,
// para comparar la forma serializada y no los tipos Go). Si diverge:
//
//	regenerar: go run ./cmd/gen-demo-canon
func TestDemoCanonJSONFresco(t *testing.T) {
	path := filepath.Join("..", "..", "..", "app", "src", "data", "demo-canon.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("leer %s: %v — regenerar: go run ./cmd/gen-demo-canon", path, err)
	}
	normalize := func(v any) any {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var out any
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return out
	}
	var disk any
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("%s no es JSON válido: %v", path, err)
	}
	if !reflect.DeepEqual(normalize(disk), normalize(BuildDemoCanon())) {
		t.Fatalf("demo-canon.json desactualizado respecto al canon Go — regenerar: go run ./cmd/gen-demo-canon")
	}
}
