package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	var b strings.Builder

	// Uptime
	uptime := time.Since(s.started).Seconds()
	fmt.Fprintf(&b, "# HELP netpulse_uptime_seconds Seconds since server start.\n")
	fmt.Fprintf(&b, "# TYPE netpulse_uptime_seconds gauge\n")
	fmt.Fprintf(&b, "netpulse_uptime_seconds %.1f\n\n", uptime)

	// Router metrics from latest overview
	if s.lastOv != nil {
		ov := s.lastOv()
		if ov != nil {
			fmt.Fprintf(&b, "# HELP netpulse_router_health Health score 0-100.\n")
			fmt.Fprintf(&b, "# TYPE netpulse_router_health gauge\n")
			for _, r := range ov.Routers {
				fmt.Fprintf(&b, "netpulse_router_health{router=%q,id=%q} %d\n", r.Name, r.ID, r.Health)
			}
			b.WriteByte('\n')

			fmt.Fprintf(&b, "# HELP netpulse_router_cpu_percent CPU usage percentage.\n")
			fmt.Fprintf(&b, "# TYPE netpulse_router_cpu_percent gauge\n")
			for _, r := range ov.Routers {
				fmt.Fprintf(&b, "netpulse_router_cpu_percent{router=%q,id=%q} %d\n", r.Name, r.ID, r.CPU)
			}
			b.WriteByte('\n')

			fmt.Fprintf(&b, "# HELP netpulse_router_ram_percent RAM usage percentage.\n")
			fmt.Fprintf(&b, "# TYPE netpulse_router_ram_percent gauge\n")
			for _, r := range ov.Routers {
				fmt.Fprintf(&b, "netpulse_router_ram_percent{router=%q,id=%q} %d\n", r.Name, r.ID, r.RAM)
			}
			b.WriteByte('\n')

			fmt.Fprintf(&b, "# HELP netpulse_router_temp_celsius Temperature in Celsius.\n")
			fmt.Fprintf(&b, "# TYPE netpulse_router_temp_celsius gauge\n")
			for _, r := range ov.Routers {
				fmt.Fprintf(&b, "netpulse_router_temp_celsius{router=%q,id=%q} %d\n", r.Name, r.ID, r.Temp)
			}
			b.WriteByte('\n')

			fmt.Fprintf(&b, "# HELP netpulse_router_clients Number of connected clients.\n")
			fmt.Fprintf(&b, "# TYPE netpulse_router_clients gauge\n")
			for _, r := range ov.Routers {
				fmt.Fprintf(&b, "netpulse_router_clients{router=%q,id=%q} %d\n", r.Name, r.ID, r.Clients)
			}
			b.WriteByte('\n')

			// WAN metrics (gateway only)
			fmt.Fprintf(&b, "# HELP netpulse_wan_latency_ms WAN latency in milliseconds.\n")
			fmt.Fprintf(&b, "# TYPE netpulse_wan_latency_ms gauge\n")
			fmt.Fprintf(&b, "netpulse_wan_latency_ms %.1f\n\n", ov.WAN.LatencyMs)

			fmt.Fprintf(&b, "# HELP netpulse_wan_down_mbps WAN download speed Mbps.\n")
			fmt.Fprintf(&b, "# TYPE netpulse_wan_down_mbps gauge\n")
			fmt.Fprintf(&b, "netpulse_wan_down_mbps %.1f\n\n", ov.WAN.DownMbps)

			fmt.Fprintf(&b, "# HELP netpulse_wan_up_mbps WAN upload speed Mbps.\n")
			fmt.Fprintf(&b, "# TYPE netpulse_wan_up_mbps gauge\n")
			fmt.Fprintf(&b, "netpulse_wan_up_mbps %.1f\n\n", ov.WAN.UpMbps)

			fmt.Fprintf(&b, "# HELP netpulse_wan_loss_percent WAN packet loss percentage.\n")
			fmt.Fprintf(&b, "# TYPE netpulse_wan_loss_percent gauge\n")
			fmt.Fprintf(&b, "netpulse_wan_loss_percent %.1f\n\n", ov.WAN.LossPct)

			// Alert counts
			fmt.Fprintf(&b, "# HELP netpulse_alerts_unread Number of unread alerts.\n")
			fmt.Fprintf(&b, "# TYPE netpulse_alerts_unread gauge\n")
			fmt.Fprintf(&b, "netpulse_alerts_unread %d\n\n", ov.UnreadAlerts)
		}
	}

	// Health score
	fmt.Fprintf(&b, "# HELP netpulse_health_score Overall network health 0-100.\n")
	fmt.Fprintf(&b, "# TYPE netpulse_health_score gauge\n")
	if s.lastOv != nil {
		ov := s.lastOv()
		if ov != nil {
			fmt.Fprintf(&b, "netpulse_health_score %d\n", ov.Health.Score)
		} else {
			fmt.Fprintf(&b, "netpulse_health_score 0\n")
		}
	} else {
		fmt.Fprintf(&b, "netpulse_health_score 0\n")
	}

	_, _ = w.Write([]byte(b.String()))
}
