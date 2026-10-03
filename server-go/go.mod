module github.com/gnacho/netpulse/server-go

go 1.26

require (
	github.com/SherClockHolmes/webpush-go v1.4.0
	github.com/gnacho/netpulse/agent v0.0.0
	github.com/gonzalop/mq v0.9.10
	github.com/gosnmp/gosnmp v1.44.0
	github.com/m-lab/ndt7-client-go v0.10.1
	github.com/mark3labs/mcp-go v1.1.1
	github.com/showwin/speedtest-go v1.8.3
	golang.org/x/crypto v0.55.0
	golang.org/x/net v0.58.0
	golang.org/x/text v0.41.0
	modernc.org/sqlite v1.56.0
)

// Módulo hermano en el mismo repo: sondas/parseo compartido con el agente
// nativo (SPEC-AGENTE-PILOTO §2). Solo stdlib — no arrastra dependencias.
replace github.com/gnacho/netpulse/agent => ../agent

require (
	github.com/araddon/dateparse v0.0.0-20210429162001-6b43995a97de // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/m-lab/go v0.1.76 // indirect
	github.com/m-lab/locate v0.19.0 // indirect
	github.com/m-lab/ndt-server v0.25.0 // indirect
	github.com/m-lab/tcp-info v1.9.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/sys v0.47.0 // indirect
	modernc.org/libc v1.74.4 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
