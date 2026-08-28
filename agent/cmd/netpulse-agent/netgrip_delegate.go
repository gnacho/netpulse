package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
)

const (
	netgripAddr       = "http://127.0.0.1:8080"
	netgripTokenPath  = "/etc/netgrip/executor-token"
	netgripTimeout    = 30 * time.Second
)

func readNetgripToken() (string, error) {
	data, err := os.ReadFile(netgripTokenPath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

type netgripRequest struct {
	Ops []executor.Op `json:"ops"`
}

type netgripResponse struct {
	Ok    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func delegateToNetgrip(ops []executor.Op) (executor.ApplyResult, bool) {
	token, err := readNetgripToken()
	if err != nil {
		return executor.ApplyResult{}, false
	}
	body, err := json.Marshal(netgripRequest{Ops: ops})
	if err != nil {
		return executor.ApplyResult{}, false
	}
	req, err := http.NewRequest("POST", netgripAddr+"/api/executor/apply", bytes.NewReader(body))
	if err != nil {
		return executor.ApplyResult{}, false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: netgripTimeout}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return executor.ApplyResult{}, false
	}
	defer resp.Body.Close()

	var result netgripResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return executor.ApplyResult{
			Status:     "failed",
			Error:      fmt.Sprintf("netgrip response parse: %s", err),
			DurationMs: time.Since(start).Milliseconds(),
		}, true
	}
	if !result.Ok {
		return executor.ApplyResult{
			Status:     "failed",
			Error:      fmt.Sprintf("netgrip: %s", result.Error),
			DurationMs: time.Since(start).Milliseconds(),
		}, true
	}
	return executor.ApplyResult{
		Status:     "applied",
		DurationMs: time.Since(start).Milliseconds(),
	}, true
}
