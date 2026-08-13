// update_history_test.go — GET /api/updates/history (issue #159).
package httpapi_test

import (
	"testing"
	"time"
)

func TestUpdateHistoryEndpoint(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	root := t.TempDir()
	writeDeployScript(t, root)
	writeGitHead(t, root)
	ts := makeTestServerWithUpdater(t, root)

	// Sembrar una entrada directamente (como la escribiría el updater).
	if _, err := ts.db.Exec(
		`INSERT INTO update_history
		   (event_id, ts, action, channel, version_from, version_to, initiated_by, status)
		 VALUES ('upd-123', ?, 'apply', 'rolling', 'aaa1111', 'bbb2222', 'admin', 'success')`,
		time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}

	cookie := adminCookie(t, ts)
	res := get(t, ts.URL, "/api/updates/history", cookie)
	if res.StatusCode != 200 {
		t.Fatalf("status: got %d want 200", res.StatusCode)
	}
	body := readJSON(t, res)
	hist, ok := body["history"].([]any)
	if !ok || len(hist) == 0 {
		t.Fatalf("history: %+v", body)
	}
	first := hist[0].(map[string]any)
	if first["eventId"] != "upd-123" || first["status"] != "success" ||
		first["versionFrom"] != "aaa1111" || first["versionTo"] != "bbb2222" {
		t.Errorf("entrada: %+v", first)
	}
	if first["initiatedBy"] != "admin" {
		t.Errorf("initiatedBy: %+v", first["initiatedBy"])
	}
}

func TestUpdateHistoryRequiresAdmin(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	root := t.TempDir()
	writeDeployScript(t, root)
	writeGitHead(t, root)
	ts := makeTestServerWithUpdater(t, root)
	admin := adminCookie(t, ts)
	userCookie := createUserAndLogin(t, ts.URL, admin, "viewer", "clave12345", "user")
	if got := get(t, ts.URL, "/api/updates/history", userCookie).StatusCode; got != 403 {
		t.Errorf("GET history como user: got %d want 403", got)
	}
}
