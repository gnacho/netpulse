package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestTokensCRUD(t *testing.T) {
	ts := makeTestServer(t)
	defer ts.Close()
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	createBody, _ := json.Marshal(map[string]any{
		"name": "test-token", "scope": "read", "expiresInDays": 0,
	})

	var created struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	{
		req, _ := http.NewRequest("POST", ts.URL+"/api/tokens", bytes.NewReader(createBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "session="+cookie)
		resp, _ := http.DefaultClient.Do(req)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create: expected 201, got %d", resp.StatusCode)
		}
		_ = json.NewDecoder(resp.Body).Decode(&created)
		if created.ID == "" || created.Token == "" {
			t.Fatal("create: empty id or token")
		}
	}

	{
		req, _ := http.NewRequest("GET", ts.URL+"/api/tokens", nil)
		req.Header.Set("Cookie", "session="+cookie)
		resp, _ := http.DefaultClient.Do(req)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list: expected 200, got %d", resp.StatusCode)
		}
		var body struct {
			Tokens []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"tokens"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if len(body.Tokens) != 1 || body.Tokens[0].Name != "test-token" {
			t.Fatalf("list: unexpected: %+v", body.Tokens)
		}
	}

	{
		req, _ := http.NewRequest("DELETE", ts.URL+"/api/tokens/"+created.ID, nil)
		req.Header.Set("Cookie", "session="+cookie)
		resp, _ := http.DefaultClient.Do(req)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("delete: expected 204, got %d", resp.StatusCode)
		}
	}

	{
		req, _ := http.NewRequest("GET", ts.URL+"/api/tokens", nil)
		req.Header.Set("Cookie", "session="+cookie)
		resp, _ := http.DefaultClient.Do(req)
		defer resp.Body.Close()
		var body struct {
			Tokens []struct{ ID string } `json:"tokens"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if len(body.Tokens) != 0 {
			t.Fatalf("after delete: expected 0, got %d", len(body.Tokens))
		}
	}
}

func TestTokenBearerAuth(t *testing.T) {
	ts := makeTestServer(t)
	defer ts.Close()
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	createBody, _ := json.Marshal(map[string]any{
		"name": "bearer-test", "scope": "read", "expiresInDays": 0,
	})
	req, _ := http.NewRequest("POST", ts.URL+"/api/tokens", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "session="+cookie)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	var created struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	if created.Token == "" {
		t.Fatal("no token created")
	}

	req, _ = http.NewRequest("GET", ts.URL+"/api/overview", nil)
	req.Header.Set("Authorization", "Bearer "+created.Token)
	resp2, _ := http.DefaultClient.Do(req)
	defer resp2.Body.Close()
	if resp2.StatusCode == http.StatusUnauthorized {
		t.Fatal("bearer token should authenticate, got 401")
	}
}

func TestTokenBearerInvalid(t *testing.T) {
	ts := makeTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/overview", nil)
	req.Header.Set("Authorization", "Bearer np_invalidtoken")
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid bearer should get 401, got %d", resp.StatusCode)
	}
}
