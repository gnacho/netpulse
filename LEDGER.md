# LEDGER — Sweep NetPulse (28-Ago-2026)

Evidencia medida por hoja. Nada se marca done sin fila aquí.

## HOJA A — #305 per-port counters
- commit 8b3b9c0 (feat/305-switch-native-poll, 13 ficheros +426/-31)
- go test -race server-go ./internal/... : TODAS ok (22 paquetes, 28-Ago ~09:58)
- go test -race agent ./... : TODAS ok
- npm run build: bundle index-C3_fe5Yf.js, 0 errores tsc; eslint 0 errors (5 warnings preexistentes)
- Deploy CT 200: health {"version":"2.15.0-305","mode":"live","db":"ok"} (28-Ago 08:03 UTC)
- PATH AGENTE: GET /api/routers/gl-inet-gl-mt6000 → ports wan/eth1 rxBps=3759543 txBps=32002730 rxErr=81; lan5 31.35 Mbps; rt2 lan4 109310/48755 (2 dispositivos)
- PATH SSH: agente flint2 parado (cron watchdog fuera) >3 min → degrade Tier 0 → detail con rates wan 2546154/37161082, lan5 29568724 bps. Restaurado: cron=1 línea, agente fresh 2.15.0-305
- UI: Playwright tooltip LAN1 "TRÁFICO | ↓ 2 kbps · ↑ 9 kbps" + cuerpo "↓2 kbps ↑9 kbps", 0 errores JS (portpanel-305.png)
- Observación: rt2 volvió a la red y su agente quedó 2.15.0-305 (upgrade-all externo o evento encolado); fresh:True

## HOJA B — #281 + #282 matching agente↔router + estado combinado
- commit 4863f45 en sweep/B; PR #317 mergeado en main 8b417da
- Cambios: AgentRegistry.MatchRouter (slug/hostname/MAC), pollRouterAgent usa fuzzy match, alerta SSH-auth-fail con agente vivo, GET /api/agents expone routerId, frontend useAgentFor preferencia routerId.
- Tests: TestAgentRegistryMatchRouterByHostname, ByMac, Stale; TestLiveAgentFuzzyMatchSkipsSSH; TestLiveAgentSSHAuthFailAlert; TestAgentsListResolvesRouterIdByHostname.
- Verificación: go build+vet ok; go test -race ./internal/... TODAS ok (162s); npm run build ok; npm run lint 0 errors (5 warnings preexistentes).

## HOJA C — #290 CI cleanup races + #298 cierre documental
- commit ca9624a en sweep/C; PR #318 mergeado en main e16cf10
- Validación yaml.safe_load OK; bash -n OK; #298 cerrado con comentario de verificación (file:line en rearmer.go, agent.go, upgrade.go, AgentsSection.tsx, RouterInfo.tsx).
- Nota: el push del workflow requirió SSH porque el token OAuth de gh carece de scope workflow.

## HOJA D — pendiente
## HOJA E — pendiente
## HOJA F — pendiente
## HOJA G — pendiente
## HOJA H — pendiente
## HOJA I — pendiente
