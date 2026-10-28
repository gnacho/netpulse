package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeTokenValidator struct {
	user  *User
	scope string
}

func (f fakeTokenValidator) ValidateBearer(string) (*User, string) {
	return f.user, f.scope
}

func scopeHandler(scope string, admin bool) http.Handler {
	var next http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	if admin {
		next = RequireAdmin(next)
	}
	return RequireAuth(nil, "secret", fakeTokenValidator{
		user:  &User{ID: 1, Username: "admin", Role: "admin"},
		scope: scope,
	}, next)
}

func TestScopeAllowsMethodFailClosed(t *testing.T) {
	admin := &User{ID: 1, Username: "admin", Role: "admin"}
	for _, tc := range []struct {
		name   string
		scope  string
		method string
		want   int
	}{
		{"read get", ScopeRead, http.MethodGet, http.StatusOK},
		{"read head", ScopeRead, http.MethodHead, http.StatusOK},
		{"read post", ScopeRead, http.MethodPost, http.StatusForbidden},
		{"read delete", ScopeRead, http.MethodDelete, http.StatusForbidden},
		{"write post", ScopeWrite, http.MethodPost, http.StatusOK},
		{"admin post", ScopeAdmin, http.MethodPost, http.StatusOK},
		{"unknown scope get", "bogus", http.MethodGet, http.StatusForbidden},
		{"empty scope get", "", http.MethodGet, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := RequireAuth(nil, "secret", fakeTokenValidator{user: admin, scope: tc.scope},
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
			req := httptest.NewRequest(tc.method, "/api/overview", nil)
			req.Header.Set("Authorization", "Bearer np_test")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("got %d want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestRequireAdminRejectsNonAdminTokenScope(t *testing.T) {
	for _, tc := range []struct {
		scope string
		want  int
	}{
		{ScopeRead, http.StatusForbidden},
		{ScopeWrite, http.StatusForbidden},
		{ScopeAdmin, http.StatusOK},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/settings/backup", nil)
			req.Header.Set("Authorization", "Bearer np_test")
			rec := httptest.NewRecorder()
			scopeHandler(tc.scope, true).ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("got %d want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
