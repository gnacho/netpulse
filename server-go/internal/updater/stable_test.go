// stable_test.go — auto-apply en layout estable (#480): capacidad (unidad
// root instalada), flujo de staging feliz (download+verify+marcador),
// confirmación en el arranque siguiente (patrón #444) y fallos (checksum).
package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// overrideStableUnit fuerza el resultado de la detección de la unidad root
// durante el test.
func overrideStableUnit(t *testing.T, installed bool) {
	t.Helper()
	old := stableUnitInstalled
	stableUnitInstalled = func() bool { return installed }
	t.Cleanup(func() { stableUnitInstalled = old })
}

// fakeReleaseAsset construye un tarball con un binario "netpulse" dentro y
// devuelve (bytes del tarball, sha256 hex del tarball).
func fakeReleaseAsset(t *testing.T, binContent string) ([]byte, string) {
	t.Helper()
	var buf strings.Builder
	gz := gzip.NewWriter(&bufWriter{&buf})
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "netpulse", Mode: 0o755, Size: int64(len(binContent))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(binContent)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	raw := []byte(buf.String())
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:])
}

// bufWriter adapta strings.Builder a io.Writer sin copiar el contenido dos
// veces (Write recibe la porción, no el builder entero).
type bufWriter struct{ b *strings.Builder }

func (w *bufWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

// serveStableRelease monta un httptest que hace de GitHub: APIBase para
// /releases/latest y downloadBase para los assets del tag dado.
func serveStableRelease(t *testing.T, tag, asset, tgzSum string, tgzBytes []byte, sumLine string) {
	t.Helper()
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			return // probe de readiness: solo comprueba conectividad
		}
		// Compare del changelog (issue #490): se tolera sin fallar el test.
		if strings.Contains(r.URL.Path, "/compare/") {
			fmt.Fprint(w, `{"commits":[]}`)
			return
		}
		if r.URL.Path != "/repos/owner/netpulse/releases/latest" {
			t.Errorf("API path inesperado: %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"tag_name":%q,"name":%q,"body":"notas de %s"}`, tag, tag, tag)
	})
	dl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/owner/netpulse/releases/download/" + tag
		if !strings.HasPrefix(r.URL.Path, want+"/") {
			t.Errorf("download path inesperado: %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		switch filepath.Base(r.URL.Path) {
		case asset:
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tgzBytes)
		case "checksums.txt":
			fmt.Fprintf(w, "%s  %s\n", sumLine, asset)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(dl.Close)
	old := downloadBase
	downloadBase = dl.URL
	t.Cleanup(func() { downloadBase = old })
}

func waitStableMarker(t *testing.T, dataDir string) string {
	t.Helper()
	marker := filepath.Join(dataDir, stableMarkerName)
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			b, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout esperando el marcador %s (status: %+v)", marker, func() any {
				u := marker
				return u
			}())
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// Camino feliz: capacidad estable → Check (updateAvailable) → Apply →
// marcador con sha256 y binario en escena → "arranque siguiente" con la
// versión nueva y el marcador de éxito del helper → pendingApply + historial
// success (patrón #444 extendido a boot-time).
func TestStableApplyHappyPath(t *testing.T) {
	overrideStableUnit(t, true)
	bin := "#!/bin/sh\necho netpulse-9.9.9\n"
	tgz, tgzSum := fakeReleaseAsset(t, bin)
	asset := fmt.Sprintf("%s_9.9.9_linux_%s.tar.gz", stableBinName, runtime.GOARCH)
	serveStableRelease(t, "v9.9.9", asset, tgzSum, tgz, tgzSum)

	dataDir := t.TempDir()
	dbh := openDB(t)
	u := New(t.TempDir(), "owner/netpulse", "", "2.0.0", dbh).WithDataDir(dataDir)

	st := u.Check(context.Background())
	if st.Error != nil || !st.UpdateAvailable || !st.CanApply {
		t.Fatalf("check: %+v", st)
	}
	if st.Mode != "stable" {
		t.Fatalf("mode: %s", st.Mode)
	}
	if !u.Apply() {
		t.Fatal("apply debería arrancar")
	}

	marker := waitStableMarker(t, dataDir)
	if !strings.Contains(marker, "target=v9.9.9") {
		t.Fatalf("marcador sin target: %q", marker)
	}
	stagedPath := fieldOf(marker, "staged")
	wantSHA := fieldOf(marker, "sha256")
	gotSHA, err := fileSHA256(stagedPath)
	if err != nil {
		t.Fatalf("binario en escena no accesible: %v", err)
	}
	if gotSHA != wantSHA {
		t.Fatalf("sha del binario en escena: got %s want %s", gotSHA, wantSHA)
	}

	// Simular al helper: swap OK → marcador de éxito con el objetivo y
	// limpieza del marcador disparador (el proceso muere con el restart).
	if err := os.WriteFile(filepath.Join(dataDir, stableAppliedName), []byte("v9.9.9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(dataDir, stableMarkerName))

	// "Arranque siguiente": updater nuevo con la versión ya actualizada.
	u.Stop() // libera la goroutine del apply (sigue esperando el restart)
	u2 := New(t.TempDir(), "owner/netpulse", "", "9.9.9", dbh).WithDataDir(dataDir)
	st2 := u2.Status()
	if st2.PendingApply == nil {
		t.Fatal("pendingApply debería confirmarse en el arranque con marcador de éxito")
	}
	if st2.PendingApply.From != "2.0.0" || st2.PendingApply.To != "9.9.9" {
		t.Fatalf("pendingApply: %+v", st2.PendingApply)
	}
	hist, err := ListHistory(dbh, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) == 0 || hist[0].Status != "success" {
		t.Fatalf("historial: esperaba success re-marcado, got %+v", hist)
	}
}

// Sin el marcador de éxito del helper (apply murió a medias): el arranque
// NO confirma y el historial queda fallido.
func TestStableApplyWithoutHelperMarkerIsSilent(t *testing.T) {
	overrideStableUnit(t, true)
	bin := "x"
	tgz, tgzSum := fakeReleaseAsset(t, bin)
	asset := fmt.Sprintf("%s_9.9.9_linux_%s.tar.gz", stableBinName, runtime.GOARCH)
	serveStableRelease(t, "v9.9.9", asset, tgzSum, tgz, tgzSum)

	dataDir := t.TempDir()
	dbh := openDB(t)
	u := New(t.TempDir(), "owner/netpulse", "", "2.0.0", dbh).WithDataDir(dataDir)
	if st := u.Check(context.Background()); !st.UpdateAvailable {
		t.Fatalf("check: %+v", st)
	}
	if !u.Apply() {
		t.Fatal("apply debería arrancar")
	}
	_ = waitStableMarker(t, dataDir)
	// El helper NO llega a escribir .update-applied (p.ej. sha mismatch).
	u.Stop()

	u2 := New(t.TempDir(), "owner/netpulse", "", "9.9.9", dbh).WithDataDir(dataDir)
	if u2.Status().PendingApply != nil {
		t.Fatal("sin marcador del helper no debería confirmarse")
	}
	hist, err := ListHistory(dbh, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) == 0 || hist[0].Status != "failed" {
		t.Fatalf("historial: esperaba failed, got %+v", hist)
	}
}

// Checksum del tarball incorrecto: el apply falla, no hay marcador y el
// pending se limpia (#161).
func TestStableApplyChecksumMismatch(t *testing.T) {
	overrideStableUnit(t, true)
	bin := "x"
	tgz, _ := fakeReleaseAsset(t, bin)
	asset := fmt.Sprintf("%s_9.9.9_linux_%s.tar.gz", stableBinName, runtime.GOARCH)
	serveStableRelease(t, "v9.9.9", asset, "deadbeef", tgz, strings.Repeat("0", 64))

	dataDir := t.TempDir()
	dbh := openDB(t)
	u := New(t.TempDir(), "owner/netpulse", "", "2.0.0", dbh).WithDataDir(dataDir)
	if st := u.Check(context.Background()); !st.UpdateAvailable {
		t.Fatalf("check: %+v", st)
	}
	if !u.Apply() {
		t.Fatal("apply debería arrancar")
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if st := u.Status(); st.Error != nil && *st.Error == "stable_checksum_mismatch" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout esperando stable_checksum_mismatch: %+v", u.Status())
		}
		time.Sleep(25 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(dataDir, stableMarkerName)); !os.IsNotExist(err) {
		t.Fatal("no debería quedar marcador tras un mismatch")
	}
	if _, ok := readPendingApply(dbh); ok {
		t.Fatal("pendingApply debe limpiarse en un fallo (#161)")
	}
	hist, err := ListHistory(dbh, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) == 0 || hist[0].Status != "failed" || hist[0].Error == nil || *hist[0].Error != "stable_checksum_mismatch" {
		t.Fatalf("historial: %+v", hist[0])
	}
}

// Con la unidad root instalada, el layout estable pasa a canApply (#480);
// sin ella, sigue !canApply (comportamiento previo intacto).
func TestStableCapabilityWithUnit(t *testing.T) {
	overrideStableUnit(t, true)
	u := New(t.TempDir(), "owner/netpulse", "", "1.0.0", nil).WithDataDir(t.TempDir())
	if !u.CanApply() || u.mode != "stable" {
		t.Fatalf("estable+unidad debería ser stable/canApply: %+v", u.Status())
	}

	overrideStableUnit(t, false)
	u2 := New(t.TempDir(), "owner/netpulse", "", "1.0.0", nil).WithDataDir(t.TempDir())
	if u2.CanApply() || u2.mode != "stable" {
		t.Fatalf("estable sin unidad debería ser stable/!canApply")
	}
}

// El marcador .update-applied vive en el dataDir en estable y en el
// repoRoot en rolling.
func TestAppliedMarkerPathByMode(t *testing.T) {
	uRolling := New(t.TempDir(), "owner/netpulse", "", "1.0.0", nil)
	writeDeployScript(t, uRolling.repoRoot)
	// recrear para que la detección pille el update.sh
	uRolling = New(uRolling.repoRoot, "owner/netpulse", "", "1.0.0", nil)
	if !strings.HasSuffix(uRolling.appliedMarkerPath(), filepath.Join(uRolling.repoRoot, ".update-applied")) {
		t.Fatalf("rolling: %s", uRolling.appliedMarkerPath())
	}
	dataDir := t.TempDir()
	uStable := New(t.TempDir(), "owner/netpulse", "", "1.0.0", nil).WithDataDir(dataDir)
	if uStable.appliedMarkerPath() != filepath.Join(dataDir, stableAppliedName) {
		t.Fatalf("estable: %s", uStable.appliedMarkerPath())
	}
}

func fieldOf(marker, key string) string {
	for _, line := range strings.Split(marker, "\n") {
		if strings.HasPrefix(line, key+"=") {
			return strings.TrimPrefix(line, key+"=")
		}
	}
	return ""
}
