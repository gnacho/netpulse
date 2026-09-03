// updater_compare_test.go — changelog humano del asistente (issue #490):
// el Check expone los commits current→latest (compare de GitHub) en ambos
// modos, tolera fallos del compare y cachea por par (current, latest).
package updater

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// mockCompare: respuesta del compare con 3 commits oldest→newest (GitHub los
// devuelve en orden cronológico; el updater los invierte).
const compareBody = `{
  "commits": [
    {"sha": "1111111111111111", "commit": {"message": "fix(wifi): parse freq as float (#475)\n\ncon cuerpo"}},
    {"sha": "2222222222222222", "commit": {"message": "feat(routers): one-click agent install (#483)"}},
    {"sha": "deadbee1234567890", "commit": {"message": "feat: newest\n\nlong body"}}
  ]
}`

func TestCheckFetchesCompareCommits(t *testing.T) {
	root := t.TempDir()
	writeDeployScript(t, root)
	short := writeGitHead(t, root)
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/netpulse/commits/main":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"sha":"deadbee1234567890","commit":{"message":"feat: newest\n\nlong body"}}`)
		case strings.HasPrefix(r.URL.Path, "/repos/owner/netpulse/compare/"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, compareBody)
		default:
			t.Errorf("path inesperado: %s", r.URL.Path)
		}
	})
	u := New(root, "owner/netpulse", "", "2.0.0", nil)
	st := u.Check(context.Background())
	if st.Error != nil {
		t.Fatalf("error: %v", *st.Error)
	}
	if !st.UpdateAvailable {
		t.Fatal("debería haber update")
	}
	if len(st.Commits) != 3 {
		t.Fatalf("commits: %+v", st.Commits)
	}
	// Newest first y subjects = primera línea del mensaje.
	if st.Commits[0].SHA != "deadbee" || st.Commits[0].Subject != "feat: newest" {
		t.Fatalf("commits[0]: %+v", st.Commits[0])
	}
	if st.Commits[2].SHA != "1111111" || st.Commits[2].Subject != "fix(wifi): parse freq as float (#475)" {
		t.Fatalf("commits[2]: %+v", st.Commits[2])
	}
	want := "https://github.com/owner/netpulse/compare/" + short + "...deadbee1234567890"
	if st.CompareURL != want {
		t.Fatalf("compareUrl: %q, want %q", st.CompareURL, want)
	}
}

func TestCheckCompareStableUsesTags(t *testing.T) {
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/netpulse/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"tag_name":"v2.1.0","name":"v2.1.0","body":"release notes"}`)
		case r.URL.Path == "/repos/owner/netpulse/compare/v2.0.0...v2.1.0":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"commits":[{"sha":"abcdef0123456789","commit":{"message":"feat: uno"}}]}`)
		default:
			t.Errorf("path inesperado: %s", r.URL.Path)
		}
	})
	// Sin deploy/update.sh → estable; version sin prefijo "v" (ldflags).
	u := New(t.TempDir(), "owner/netpulse", "", "2.0.0", nil)
	st := u.Check(context.Background())
	if !st.UpdateAvailable {
		t.Fatal("debería haber update 2.0.0 → 2.1.0")
	}
	if len(st.Commits) != 1 || st.Commits[0].Subject != "feat: uno" || st.Commits[0].SHA != "abcdef0" {
		t.Fatalf("commits: %+v", st.Commits)
	}
	if st.CompareURL != "https://github.com/owner/netpulse/compare/v2.0.0...v2.1.0" {
		t.Fatalf("compareUrl: %q", st.CompareURL)
	}
}

func TestCheckCompareFailureTolerated(t *testing.T) {
	root := t.TempDir()
	writeDeployScript(t, root)
	writeGitHead(t, root)
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/netpulse/commits/main":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"sha":"deadbee1234567890","commit":{"message":"feat: newest\n\nbody"}}`)
		case strings.HasPrefix(r.URL.Path, "/repos/owner/netpulse/compare/"):
			http.Error(w, "boom", http.StatusInternalServerError)
		case r.URL.Path == "/repos/owner/netpulse/releases":
			// #404: body no vacío en el mock → no llega aquí, pero por si acaso.
			fmt.Fprint(w, `[]`)
		default:
			t.Errorf("path inesperado: %s", r.URL.Path)
		}
	})
	u := New(root, "owner/netpulse", "", "2.0.0", nil)
	st := u.Check(context.Background())
	if st.Error != nil {
		t.Fatalf("el fallo del compare no debe ser error: %v", *st.Error)
	}
	if !st.UpdateAvailable {
		t.Fatal("debería haber update pese al compare caído")
	}
	if st.Commits != nil || st.CompareURL != "" {
		t.Fatalf("compare fallido: %+v %q", st.Commits, st.CompareURL)
	}
}

func TestCheckCompareCachedPerPair(t *testing.T) {
	root := t.TempDir()
	writeDeployScript(t, root)
	writeGitHead(t, root)
	compareCalls := 0
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/netpulse/commits/main":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"sha":"deadbee1234567890","commit":{"message":"feat: newest\n\nbody"}}`)
		case strings.HasPrefix(r.URL.Path, "/repos/owner/netpulse/compare/"):
			compareCalls++
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, compareBody)
		default:
			t.Errorf("path inesperado: %s", r.URL.Path)
		}
	})
	u := New(root, "owner/netpulse", "", "2.0.0", nil)
	st1 := u.Check(context.Background())
	if len(st1.Commits) != 3 {
		t.Fatalf("commits primer check: %+v", st1.Commits)
	}
	st2 := u.Check(context.Background())
	if compareCalls != 1 {
		t.Fatalf("el compare se reconsultó con el mismo par: %d llamadas", compareCalls)
	}
	if len(st2.Commits) != 3 {
		t.Fatalf("commits cacheados: %+v", st2.Commits)
	}
}
