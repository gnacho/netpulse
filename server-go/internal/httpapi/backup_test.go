// backup_test.go — issue #218: el download de la BD completa queda
// restringido a admin y avisa explícitamente de que contiene secrets kv.
// Issue #741: BackupDue (frecuencia y hora fija) y validación del time.
package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/httpapi"
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

func TestBackupDueFrequency(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		cfg     httpapi.BackupConfig
		lastRun time.Time
		want    bool
	}{
		{"disabled never", httpapi.BackupConfig{Enabled: false, FrequencyH: 24}, now.Add(-48 * time.Hour), false},
		{"never ran and enabled", httpapi.BackupConfig{Enabled: true, FrequencyH: 24}, time.Time{}, true},
		{"fresh run", httpapi.BackupConfig{Enabled: true, FrequencyH: 24}, now.Add(-1 * time.Hour), false},
		{"expired run", httpapi.BackupConfig{Enabled: true, FrequencyH: 24}, now.Add(-25 * time.Hour), true},
		{"exact boundary", httpapi.BackupConfig{Enabled: true, FrequencyH: 1}, now.Add(-1 * time.Hour), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := httpapi.BackupDue(tc.cfg, tc.lastRun, now); got != tc.want {
				t.Fatalf("BackupDue = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBackupDueDailyTime(t *testing.T) {
	cfg := httpapi.BackupConfig{Enabled: true, FrequencyH: 24, Time: "04:30"}
	at := func(h, m int) time.Time { return time.Date(2026, 9, 11, h, m, 0, 0, time.UTC) }
	yesterday := time.Date(2026, 9, 10, 20, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		now     time.Time
		lastRun time.Time
		want    bool
	}{
		{"before window", at(3, 0), yesterday, false},
		{"after window, not run today", at(5, 0), yesterday, true},
		{"after window, already run today", at(5, 0), at(4, 35), false},
		{"exactly at window", at(4, 30), yesterday, true},
		{"invalid time falls back to frequency", at(5, 0), yesterday, false}, // freq 24h, ayer 20:00
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := cfg
			if tc.name == "invalid time falls back to frequency" {
				c.Time = "9x:99"
			}
			if got := httpapi.BackupDue(c, tc.lastRun, tc.now); got != tc.want {
				t.Fatalf("BackupDue = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBackupTimeSettingValidation(t *testing.T) {
	srv := makeTestServer(t)
	_, adminCookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

	put := func(body string) (int, httpapi.BackupConfig) {
		req, _ := http.NewRequest("PUT", srv.URL+"/api/settings/backup", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: adminCookie})
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		defer res.Body.Close()
		var cfg httpapi.BackupConfig
		if res.StatusCode == http.StatusOK {
			if err := json.NewDecoder(res.Body).Decode(&cfg); err != nil {
				t.Fatalf("decode: %v", err)
			}
		}
		return res.StatusCode, cfg
	}

	if st, cfg := put(`{"time":"04:30"}`); st != http.StatusOK || cfg.Time != "04:30" {
		t.Fatalf("time válido: status %d cfg %+v", st, cfg)
	}
	if st, _ := put(`{"time":"25:99"}`); st != http.StatusBadRequest {
		t.Fatalf("time inválido: got %d want 400", st)
	}
	if st, cfg := put(`{"time":""}`); st != http.StatusOK || cfg.Time != "" {
		t.Fatalf("time vacío debe limpiar: status %d cfg %+v", st, cfg)
	}
}
