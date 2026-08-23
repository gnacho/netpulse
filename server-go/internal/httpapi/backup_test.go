// backup_test.go — issue #218: el download de la BD completa queda
// restringido a admin y avisa explícitamente de que contiene secrets kv.
package httpapi_test

import (
	"net/http"
	"testing"
)

func TestBackupDownloadAdminHeaderYGate(t *testing.T) {
	srv := makeTestServer(t)
	_, adminCookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

	// Admin: 200 + aviso explícito de que la descarga lleva secrets.
	req, _ := http.NewRequest("GET", srv.URL+"/api/backup/download", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: adminCookie})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("download admin: got %d want 200", res.StatusCode)
	}
	if res.Header.Get("X-Netpulse-Backup-Contains-Credentials") != "true" {
		t.Fatal("la descarga debe avisar de que contiene credenciales")
	}

	// Sin sesión: no debe servir el backup.
	res2, err := http.Get(srv.URL + "/api/backup/download")
	if err != nil {
		t.Fatalf("download sin sesión: %v", err)
	}
	res2.Body.Close()
	if res2.StatusCode == http.StatusOK {
		t.Fatal("el download sin sesión debe rechazarse")
	}
}
