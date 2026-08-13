// updater_history_test.go — historial de updates en SQLite (issue #159):
// la entrada se registra al aplicar y se finaliza con el resultado del script.
package updater

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryRecordedOnApply(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	root := t.TempDir()
	short := writeGitHead(t, root)
	if err := os.MkdirAll(filepath.Join(root, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deploy", "update.sh"),
		[]byte("#!/bin/bash\necho STEP:fetch\nsleep 0.1\ntrue\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"sha":"abc1234deadbeef","commit":{"message":"feat: algo"}}`)
	})
	dbh := openDB(t)
	u := New(root, "owner/netpulse", "", "2.0.0", dbh)
	u.Check(context.Background()) // fija current + latest
	if !u.Apply() {
		t.Fatal("apply debería arrancar")
	}
	waitDone(t, u)

	hist, err := ListHistory(dbh, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) == 0 {
		t.Fatal("sin historial tras el apply")
	}
	e := hist[0]
	if e.Status != "success" {
		t.Errorf("status: %s (entry %+v)", e.Status, e)
	}
	if e.InitiatedBy != "admin" {
		t.Errorf("initiatedBy: %s", e.InitiatedBy)
	}
	if e.Channel != "rolling" {
		t.Errorf("channel: %s", e.Channel)
	}
	if e.VersionFrom == nil || *e.VersionFrom != short {
		t.Errorf("versionFrom: %v, want %s", e.VersionFrom, short)
	}
	if e.VersionTo == nil || *e.VersionTo != "abc1234" {
		t.Errorf("versionTo: %v, want abc1234", e.VersionTo)
	}
	if e.DurationMS == nil {
		t.Error("durationMs no debería ser nil")
	}
	if e.Error != nil {
		t.Errorf("error inesperado: %v", *e.Error)
	}
}

func TestHistoryFailedOnApplyError(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	root := t.TempDir()
	writeGitHead(t, root)
	if err := os.MkdirAll(filepath.Join(root, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deploy", "update.sh"),
		[]byte("#!/bin/bash\necho STEP:fetch\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	dbh := openDB(t)
	u := New(root, "owner/netpulse", "", "2.0.0", dbh)
	if !u.Apply() {
		t.Fatal("apply debería arrancar")
	}
	waitDone(t, u)

	hist, _ := ListHistory(dbh, 10)
	if len(hist) == 0 {
		t.Fatal("sin historial")
	}
	e := hist[0]
	if e.Status != "failed" {
		t.Errorf("status: %s, want failed (%+v)", e.Status, e)
	}
	if e.Error == nil || *e.Error != "update_exit_3" {
		t.Errorf("error: %v, want update_exit_3", e.Error)
	}
}

func TestHistoryInterruptedFinalizedOnStartup(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	root := t.TempDir()
	writeDeployScript(t, root)
	writeGitHead(t, root)
	dbh := openDB(t)
	// Entrada 'running' residual: simula un apply cortado por el reinicio.
	if _, err := dbh.Exec(
		`INSERT INTO update_history (event_id, ts, action, channel, initiated_by, status)
		 VALUES ('upd-test', ?, 'apply', 'rolling', 'admin', 'running')`,
		time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	New(root, "owner/netpulse", "", "2.0.0", dbh) // arranque → finaliza la pendiente
	hist, err := ListHistory(dbh, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) == 0 {
		t.Fatal("sin historial")
	}
	e := hist[0]
	if e.Status != "failed" {
		t.Errorf("status: %s, want failed (%+v)", e.Status, e)
	}
	if e.Error == nil || *e.Error != "interrupted_by_restart" {
		t.Errorf("error: %v, want interrupted_by_restart", e.Error)
	}
}
