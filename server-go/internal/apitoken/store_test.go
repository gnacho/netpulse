package apitoken

import (
	"os"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

func setup(t *testing.T) (*Store, *db.DB) {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := EnsureSchema(d); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AuthUser: "admin", AuthPass: "test123456"}
	if err := auth.EnsureUsers(d, cfg); err != nil {
		t.Fatal(err)
	}
	return NewStore(d, "test-secret"), d
}

func TestCreateAndValidate(t *testing.T) {
	s, _ := setup(t)
	id, raw, err := s.Create("my-token", ScopeRead, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" || raw == "" {
		t.Fatal("empty id or raw")
	}
	if raw[:3] != TokenPrefix {
		t.Fatalf("raw should start with %s, got %s", TokenPrefix, raw[:3])
	}
	tok := s.Validate(raw)
	if tok == nil {
		t.Fatal("validate returned nil")
	}
	if tok.Name != "my-token" || tok.Scope != ScopeRead {
		t.Fatalf("unexpected token: %+v", tok)
	}
}

func TestValidateExpired(t *testing.T) {
	s, d := setup(t)
	_, raw, err := s.Create("exp", ScopeRead, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	tok := s.Validate(raw)
	if tok == nil {
		t.Fatal("should be valid now")
	}
	_, _ = d.Exec("UPDATE api_tokens SET expires_at = 1 WHERE name = 'exp'")
	tok = s.Validate(raw)
	if tok != nil {
		t.Fatal("should be nil after expiry")
	}
}

func TestValidateBadToken(t *testing.T) {
	s, _ := setup(t)
	if tok := s.Validate("np_bogus"); tok != nil {
		t.Fatal("should be nil for bogus token")
	}
	if tok := s.Validate("not-a-token"); tok != nil {
		t.Fatal("should be nil for non-prefixed token")
	}
}

func TestListAndDelete(t *testing.T) {
	s, _ := setup(t)
	s.Create("t1", ScopeRead, 1, 0)
	s.Create("t2", ScopeWrite, 1, 0)
	tokens, err := s.List(1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}
	if err := s.Delete(tokens[0].ID, 1, false); err != nil {
		t.Fatal(err)
	}
	tokens, _ = s.List(1, false)
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token after delete, got %d", len(tokens))
	}
}

func TestValidateBearerScope(t *testing.T) {
	s, _ := setup(t)
	_, raw, _ := s.Create("admin-tok", ScopeAdmin, 1, 0)
	user, scope := s.ValidateBearer(raw)
	if user == nil {
		t.Fatal("admin token should validate")
	}
	if scope != ScopeAdmin {
		t.Fatalf("expected admin scope, got %s", scope)
	}
	if user.Role != "admin" {
		t.Fatalf("expected admin user, got %s", user.Role)
	}
}

func TestValidateBearerRead(t *testing.T) {
	s, _ := setup(t)
	_, raw, _ := s.Create("read-tok", ScopeRead, 1, 0)
	user, scope := s.ValidateBearer(raw)
	if user == nil {
		t.Fatal("read token should validate")
	}
	if scope != ScopeRead {
		t.Fatalf("expected read scope, got %s", scope)
	}
}

func TestInvalidScope(t *testing.T) {
	s, _ := setup(t)
	_, _, err := s.Create("bad", "superadmin", 1, 0)
	if err == nil {
		t.Fatal("should reject invalid scope")
	}
}

func TestEnsureSchemaIdempotent(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := EnsureSchema(d); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSchema(d); err != nil {
		t.Fatal("second call should be idempotent")
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
