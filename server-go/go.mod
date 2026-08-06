module github.com/gnacho/netpulse/server-go

go 1.25.0

require (
	github.com/SherClockHolmes/webpush-go v1.4.0
	github.com/gnacho/netpulse/agent v0.0.0
	golang.org/x/crypto v0.54.0
	golang.org/x/text v0.40.0
	modernc.org/sqlite v1.55.0
)

// Módulo hermano en el mismo repo: sondas/parseo compartido con el agente
// nativo (SPEC-AGENTE-PILOTO §2). Solo stdlib — no arrastra dependencias.
replace github.com/gnacho/netpulse/agent => ../agent

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.47.0 // indirect
	modernc.org/libc v1.74.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
