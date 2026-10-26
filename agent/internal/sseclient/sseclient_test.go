package sseclient

import (
	"testing"
	"time"
)

// #1563: una conexión que llegó a establecerse reinicia el backoff, sin
// importar cuántos cortes limpios hubiera antes.
func TestRetryDelayResetsAfterCleanDisconnect(t *testing.T) {
	c := &Client{minRetry: 5 * time.Second, maxRetry: 5 * time.Minute}

	for _, current := range []time.Duration{5 * time.Second, 40 * time.Second, 5 * time.Minute} {
		delay, next := c.retryDelay(current, true)
		if delay != c.minRetry || next != c.minRetry {
			t.Fatalf("corte limpio con current=%s: delay=%s next=%s, quería %s/%s",
				current, delay, next, c.minRetry, c.minRetry)
		}
	}
}

// Solo los fallos consecutivos hacen crecer la espera, y nunca por encima de
// maxRetry.
func TestRetryDelayGrowsOnFailures(t *testing.T) {
	c := &Client{minRetry: 5 * time.Second, maxRetry: 5 * time.Minute}

	cases := []struct {
		current time.Duration
		delay   time.Duration
		next    time.Duration
	}{
		{5 * time.Second, 5 * time.Second, 10 * time.Second},
		{10 * time.Second, 10 * time.Second, 20 * time.Second},
		{4 * time.Minute, 4 * time.Minute, 5 * time.Minute},
		{5 * time.Minute, 5 * time.Minute, 5 * time.Minute},
	}
	for _, tc := range cases {
		delay, next := c.retryDelay(tc.current, false)
		if delay != tc.delay || next != tc.next {
			t.Fatalf("fallo con current=%s: delay=%s next=%s, quería %s/%s",
				tc.current, delay, next, tc.delay, tc.next)
		}
	}
}
