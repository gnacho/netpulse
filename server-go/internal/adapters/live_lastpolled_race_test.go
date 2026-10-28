package adapters

import (
	"fmt"
	"sync"
	"testing"
)

func TestAuditFleetFdbUplinksConcurrentSetRouters(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		polled := make(map[string]*routerPolled, 10000)
		for i := 0; i < 10000; i++ {
			id := fmt.Sprintf("router-%d", i)
			polled[id] = &routerPolled{cfg: RouterConfig{ID: id}}
		}
		live := &Live{lastPolled: polled}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_ = live.FleetFdbUplinks()
		}()
		go func() {
			defer wg.Done()
			<-start
			live.SetRouters(nil)
		}()
		close(start)
		wg.Wait()
	}
}
