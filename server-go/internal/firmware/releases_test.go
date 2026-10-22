// releases_test.go - disponibilidad de releases OpenWrt (#1444).
package firmware

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d.DB
}

func TestParseReleasesIndex(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
<a href="../">../</a>
<a href="21.02.7/">21.02.7/</a>
<a href="24.10.0/">24.10.0/</a>
<a href="23.05.5/">23.05.5/</a>
<a href="24.10.1/">24.10.1/</a>
<a href="25.12.0-rc1/">25.12.0-rc1/</a>
<a href="snapshots/">snapshots/</a>
<a href="fa.html">Free software</a>
</body></html>`
	got := ParseReleasesIndex(html)
	want := []string{"25.12.0-rc1", "24.10.1", "24.10.0", "23.05.5", "21.02.7"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("parse: %v, want %v", got, want)
	}
}

func TestParseReleasesIndexRCOrdersBeforeFinal(t *testing.T) {
	got := ParseReleasesIndex(`<a href="24.10.0/">24.10.0/</a><a href="24.10.0-rc3/">24.10.0-rc3/</a>`)
	if len(got) != 2 || got[0] != "24.10.0" || got[1] != "24.10.0-rc3" {
		t.Fatalf("orden rc: %v", got)
	}
}

func TestParseReleasesIndexEmpty(t *testing.T) {
	if got := ParseReleasesIndex("<html>nada</html>"); len(got) != 0 {
		t.Fatalf("sin entradas: %v", got)
	}
}

func TestFetchReleasesOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<a href="24.10.1/">24.10.1/</a><a href="23.05.5/">23.05.5/</a>`)
	}))
	defer srv.Close()
	got, err := FetchReleases(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 2 || got[0] != "24.10.1" {
		t.Fatalf("versions: %v", got)
	}
}

func TestFetchReleasesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	if _, err := FetchReleases(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("debe fallar con HTTP != 200")
	}
}

func TestFetchReleasesEmptyParseFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "<html>vacio</html>")
	}))
	defer srv.Close()
	if _, err := FetchReleases(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("índice sin versiones debe fallar (no pisar la caché buena)")
	}
}

func TestReleasesIntervalDefaultAndSet(t *testing.T) {
	if got := ReleasesIntervalHours(nil); got != DefaultReleasesIntervalHours {
		t.Fatalf("default: %d", got)
	}
	if err := SetReleasesIntervalHours(nil, 0); err == nil {
		t.Fatal("rango inválido debe dar error")
	}
	if err := SetReleasesIntervalHours(nil, 24); err != nil {
		t.Fatalf("set nil-db: %v", err)
	}
}

func TestReleasesRefresherPublishesCacheOnStart(t *testing.T) {
	db := testDB(t)
	if err := SaveReleases(db, ReleasesState{Ts: time.Now().Unix(), Versions: []string{"24.10.1"}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<a href="24.10.1/">24.10.1/</a><a href="24.10.0/">24.10.0/</a>`)
	}))
	defer srv.Close()
	r := NewReleasesRefresher(db, srv.URL)
	got := make(chan []string, 1)
	r.SetSink(func(v []string, _ int64) { got <- v })
	if st, ok := LoadReleases(db); ok {
		r.emitSink(st.Versions, st.Ts)
	}
	v := <-got
	if len(v) != 1 || v[0] != "24.10.1" {
		t.Fatalf("caché publicada al arranque: %v", v)
	}
}

func TestReleasesRefresherRunNowSingleFlight(t *testing.T) {
	db := testDB(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		fmt.Fprint(w, `<a href="24.10.1/">24.10.1/</a>`)
	}))
	defer srv.Close()
	r := NewReleasesRefresher(db, srv.URL)
	if err := r.RunNow(context.Background()); err != nil {
		t.Fatalf("runnow: %v", err)
	}
	if calls != 1 {
		t.Fatalf("llamadas HTTP: %d, want 1", calls)
	}
	st, ok := LoadReleases(db)
	if !ok || len(st.Versions) != 1 || st.Versions[0] != "24.10.1" {
		t.Fatalf("caché guardada: %+v (ok=%v)", st, ok)
	}
	// Caché fresca: un tick no dispara otra descarga.
	r.tick()
	if calls != 1 {
		t.Fatalf("tick con caché fresca no debe descargar: %d llamadas", calls)
	}
}

func TestReleasesRefresherTickRefreshesStaleCache(t *testing.T) {
	db := testDB(t)
	if err := SaveReleases(db, ReleasesState{Ts: time.Now().Add(-48 * time.Hour).Unix(), Versions: []string{"23.05.5"}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		fmt.Fprint(w, `<a href="24.10.1/">24.10.1/</a>`)
	}))
	defer srv.Close()
	r := NewReleasesRefresher(db, srv.URL)
	r.tick()
	if calls != 1 {
		t.Fatalf("caché vieja debe refrescarse: %d llamadas", calls)
	}
	st, _ := LoadReleases(db)
	if st.Versions[0] != "24.10.1" {
		t.Fatalf("versiones tras refresco: %v", st.Versions)
	}
}

// #1478: un fallo de red no toca la lista ni su Ts (el gating sigue con la
// última buena), pero deja constancia del intento y suma al contador de fallos;
// el primer éxito lo reinicia.
func TestReleasesRefresherFailureKeepsListAndRecordsAttempt(t *testing.T) {
	db := testDB(t)
	good := time.Now().Add(-2 * time.Hour).Unix()
	if err := SaveReleases(db, ReleasesState{Ts: good, Versions: []string{"24.10.1"}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := NewReleasesRefresher(db, srv.URL)
	now := time.Now()
	r.now = func() time.Time { return now }
	if err := r.RunNow(context.Background()); err == nil {
		t.Fatal("esperaba error del refresco")
	}
	st, ok := LoadReleasesRecord(db)
	if !ok {
		t.Fatal("el registro del intento no se guardó")
	}
	if st.Ts != good || len(st.Versions) != 1 || st.Versions[0] != "24.10.1" {
		t.Fatalf("la lista buena cambió tras el fallo: %+v", st)
	}
	if st.LastAttemptTs != now.Unix() || st.LastError == "" || st.FailCount != 1 {
		t.Fatalf("intento no registrado: %+v", st)
	}
	// Segundo fallo: el contador sube.
	if err := r.RunNow(context.Background()); err == nil {
		t.Fatal("esperaba error en el segundo intento")
	}
	if st, _ = LoadReleasesRecord(db); st.FailCount != 2 {
		t.Fatalf("fallos consecutivos %d, want 2", st.FailCount)
	}
	// Éxito: lista nueva y contador a cero.
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<a href="25.12.0/">25.12.0/</a>`)
	}))
	defer okSrv.Close()
	r2 := NewReleasesRefresher(db, okSrv.URL)
	r2.now = func() time.Time { return now.Add(time.Minute) }
	if err := r2.RunNow(context.Background()); err != nil {
		t.Fatalf("refresco bueno: %v", err)
	}
	st, _ = LoadReleasesRecord(db)
	if st.FailCount != 0 || st.LastError != "" || len(st.Versions) != 1 || st.Versions[0] != "25.12.0" {
		t.Fatalf("tras el éxito: %+v", st)
	}
}

// #1478: 5 min doblando hasta 1 h, y sin espera cuando no hay fallos.
func TestReleasesBackoff(t *testing.T) {
	cases := []struct {
		fails int
		want  time.Duration
	}{
		{0, 0}, {-1, 0},
		{1, 5 * time.Minute}, {2, 10 * time.Minute}, {3, 20 * time.Minute},
		{4, 40 * time.Minute}, {5, time.Hour}, {9, time.Hour},
	}
	for _, c := range cases {
		if got := ReleasesBackoff(c.fails); got != c.want {
			t.Errorf("ReleasesBackoff(%d) = %v, want %v", c.fails, got, c.want)
		}
	}
}

// #1478: el tick respeta el backoff tras un fallo (antes reintentaba cada 30 s).
func TestReleasesRefresherTickHonoursBackoff(t *testing.T) {
	db := testDB(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := NewReleasesRefresher(db, srv.URL)
	base := time.Now()
	now := base
	r.now = func() time.Time { return now }

	r.tick() // primer intento (sin caché) -> falla, backoff 5 min
	if calls != 1 {
		t.Fatalf("primer tick: %d llamadas, want 1", calls)
	}
	now = base.Add(time.Minute)
	r.tick()
	if calls != 1 {
		t.Fatalf("dentro del backoff no debe reintentar: %d llamadas", calls)
	}
	now = base.Add(6 * time.Minute)
	r.tick() // segundo intento -> falla, backoff 10 min desde aquí
	if calls != 2 {
		t.Fatalf("pasado el backoff debe reintentar: %d llamadas", calls)
	}
	now = base.Add(10 * time.Minute)
	r.tick()
	if calls != 2 {
		t.Fatalf("segundo backoff (10 min) no respetado: %d llamadas", calls)
	}
	now = base.Add(17 * time.Minute)
	r.tick()
	if calls != 3 {
		t.Fatalf("agotado el segundo backoff debe reintentar: %d llamadas", calls)
	}
}
