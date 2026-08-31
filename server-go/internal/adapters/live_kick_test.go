package adapters

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

type fakeKickRunner struct {
	calls     []struct{ host, cmd string }
	responses map[string]string
	errors    map[string]error
}

func (f *fakeKickRunner) Run(host, cmd string, timeout time.Duration) (string, error) {
	f.calls = append(f.calls, struct{ host, cmd string }{host, cmd})
	key := host + "|" + cmd
	if e, ok := f.errors[key]; ok {
		return "", e
	}
	return f.responses[key], nil
}

func TestKickUsteerClient_Success(t *testing.T) {
	runner := &fakeKickRunner{
		responses: map[string]string{
			"192.168.1.2|ubus call usteer connected_clients": `{"hostapd.phy1-ap0":{"38:c6:ce:2f:06:5a":{"signal":-50}}}`,
			fmt.Sprintf("192.168.1.2|ubus call hostapd.phy1-ap0 del_client '%s'", `{"addr":"38:C6:CE:2F:06:5A","reason":5,"deauth":true,"ban_time":120000}`): "",
		},
	}
	routers := []RouterConfig{{ID: "rt2", Name: "rt2", Host: "192.168.1.2"}}
	if err := kickUsteerClient("38:C6:CE:2F:06:5A", routers, runner); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(runner.calls))
	}
}

func TestKickUsteerClient_ClientNotFound(t *testing.T) {
	runner := &fakeKickRunner{
		responses: map[string]string{
			"192.168.1.2|ubus call usteer connected_clients": `{"hostapd.phy1-ap0":{}}`,
		},
	}
	routers := []RouterConfig{{ID: "rt2", Name: "rt2", Host: "192.168.1.2"}}
	err := kickUsteerClient("38:C6:CE:2F:06:5A", routers, runner)
	if err == nil || err.Error() != "client not found" {
		t.Fatalf("expected 'client not found', got: %v", err)
	}
}

func TestKickUsteerClient_ContinuesAfterFailure(t *testing.T) {
	runner := &fakeKickRunner{
		responses: map[string]string{
			"192.168.1.2|ubus call usteer connected_clients": `{"hostapd.phy1-ap0":{"38:c6:ce:2f:06:5a":{"signal":-50}}}`,
			"192.168.1.3|ubus call usteer connected_clients": `{"hostapd.phy1-ap0":{"38:c6:ce:2f:06:5a":{"signal":-60}}}`,
			fmt.Sprintf("192.168.1.3|ubus call hostapd.phy1-ap0 del_client '%s'", `{"addr":"38:C6:CE:2F:06:5A","reason":5,"deauth":true,"ban_time":120000}`): "",
		},
		errors: map[string]error{
			fmt.Sprintf("192.168.1.2|ubus call hostapd.phy1-ap0 del_client '%s'", `{"addr":"38:C6:CE:2F:06:5A","reason":5,"deauth":true,"ban_time":120000}`): errors.New("boom"),
		},
	}
	routers := []RouterConfig{
		{ID: "rt2", Name: "rt2", Host: "192.168.1.2"},
		{ID: "rt3", Name: "rt3", Host: "192.168.1.3"},
	}
	if err := kickUsteerClient("38:C6:CE:2F:06:5A", routers, runner); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.calls) != 4 {
		t.Fatalf("expected 4 calls, got %d", len(runner.calls))
	}
}

func TestKickUsteerClient_AllRoutersFail(t *testing.T) {
	runner := &fakeKickRunner{
		responses: map[string]string{
			"192.168.1.2|ubus call usteer connected_clients": `{"hostapd.phy1-ap0":{"38:c6:ce:2f:06:5a":{"signal":-50}}}`,
		},
		errors: map[string]error{
			fmt.Sprintf("192.168.1.2|ubus call hostapd.phy1-ap0 del_client '%s'", `{"addr":"38:C6:CE:2F:06:5A","reason":5,"deauth":true,"ban_time":120000}`): errors.New("boom"),
		},
	}
	routers := []RouterConfig{{ID: "rt2", Name: "rt2", Host: "192.168.1.2"}}
	err := kickUsteerClient("38:C6:CE:2F:06:5A", routers, runner)
	if err == nil {
		t.Fatalf("expected error")
	}
	want := "could not disconnect from rt2"
	if err.Error() != want {
		t.Fatalf("expected %q, got %q", want, err.Error())
	}
}

func TestKickUsteerClient_ConnectedClientsFails(t *testing.T) {
	runner := &fakeKickRunner{
		responses: map[string]string{
			"192.168.1.3|ubus call usteer connected_clients": `{"hostapd.phy1-ap0":{"38:c6:ce:2f:06:5a":{"signal":-50}}}`,
			fmt.Sprintf("192.168.1.3|ubus call hostapd.phy1-ap0 del_client '%s'", `{"addr":"38:C6:CE:2F:06:5A","reason":5,"deauth":true,"ban_time":120000}`): "",
		},
		errors: map[string]error{
			"192.168.1.2|ubus call usteer connected_clients": errors.New("ssh timeout"),
		},
	}
	routers := []RouterConfig{
		{ID: "rt2", Name: "rt2", Host: "192.168.1.2"},
		{ID: "rt3", Name: "rt3", Host: "192.168.1.3"},
	}
	if err := kickUsteerClient("38:C6:CE:2F:06:5A", routers, runner); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(runner.calls))
	}
}
