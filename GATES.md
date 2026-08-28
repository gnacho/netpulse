# GATES — Sweep NetPulse issues pendientes (28-Ago-2026, /unlazy)

> Gates de aceptación POR HOJA + globales de integración. CHECK = comando a
> ejecutar; EXPECT = regex que debe casar la salida; EVIDENCE = dónde queda
> registrada. Nada se reporta done sin evidencia en LEDGER.md.

## Globales (integración final)

- G-G1 build server
  CHECK: cd /tmp/opencode/np-int/server-go && go build ./...
  EXPECT: /^\s*$/
- G-G2 tests server race
  CHECK: go test -race ./internal/...
  EXPECT: /FAIL/ NO casa (todas ok)
- G-G3 tests agent race
  CHECK: cd /tmp/opencode/np-int/agent && go test -race ./...
  EXPECT: /FAIL/ NO casa
- G-G4 build front
  CHECK: cd app && npm run build
  EXPECT: /error/ NO casa en salida tsc; bundle nuevo generado
- G-G5 lint front
  CHECK: npm run lint
  EXPECT: /0 errors/ (warnings preexistentes ≤5)
- G-G6 deploy CT 200
  CHECK: curl -s http://192.168.10.200:3000/api/health (con sesión)
  EXPECT: /"ok":true/ y versión de la release
- G-G7 PRs mergeados + release tag + demo online bundle nuevo

## HOJA A — #305 per-port counters (commit 8b3b9c0, feat/305-switch-native-poll)

- A1 PR abierto con "Closes #305" y mergeado (squash)
  CHECK: gh pr view <N> -R gnacho/netpulse --json state,mergedAt -q '.state'
  EXPECT: /MERGED/
- A2 evidencia E2E ya capturada (agente: flint2+rt2 rates; SSH: degradación
  con agente parado; UI: tooltip TRÁFICO + línea ↓/↑) → LEDGER

## HOJA B — #282 matching agente↔router + #281 estado combinado

- B1 backend expone asociación (agents[].routerId o matching server-side)
  CHECK: rg 'routerId' server-go/internal/httpapi/agent.go | head -1
  EXPECT: /routerId/
- B2 test del matching (slug != router.id casa por hostname/board)
  CHECK: go test ./internal/httpapi/ -run Matching -v | tail -2
  EXPECT: /PASS/
- B3 router card con agente fresh NO muestra offline aunque SSH falle
  CHECK: test buildRouter/estado con agente fresh + lastErr SSH
  EXPECT: /PASS/
- B4 UI: useAgentFor usa la asociación nueva; commit con Closes #282 y Closes #281

## HOJA C — #290 CI cleanup races + #298 cierre documental

- C1 cleanup de assets SOLO en un job/step post-matrix (o tolerante a 404)
  CHECK: rg -n 'Limpia|cleanup|delete-asset' .github/workflows/go.yml
  EXPECT: evidencia del cambio (no per-matrix)
- C2 yaml válido
  CHECK: python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/go.yml'))"
  EXPECT: /^\s*$/
- C3 #298 verificado contra main (ErrExternalAgent + gating) y cerrado con comentario
  CHECK: gh issue view 298 -R gnacho/netpulse --json state -q '.state'
  EXPECT: /CLOSED/

## HOJA D — #303+#307+#308 alertas de puerto

- D1 motor: reglas flapping (N transiciones/ventana), unknown-MAC en boca
  con dispositivo fijo, ghost port (tráfico base → silencio), degraded link
  (speed < histórico) sobre las EthPort enriquecidas de #305
  CHECK: go test ./internal/adapters/ -run 'Port.*Alert|Alert.*Port' -v | tail -4
  EXPECT: /PASS/ (≥4 tests nuevos)
- D2 alertas deduplicadas (engine existente) y Urgent:false por defecto
- D3 commit(s) con Closes #303, Closes #307, Closes #308

## HOJA E — #302 per-port time series

- E1 tabla + muestreo con retención escalonada (patrón metrics/metrics_day)
  CHECK: rg -n 'port' server-go/internal/db/db.go | grep -i 'CREATE TABLE'
  EXPECT: /port/ (tabla nueva)
- E2 ingest desde routerPolled.ports (rates/bytes) con cap anti-salto
  CHECK: go test ./internal/adapters/ -run PortSeries -v | tail -2
  EXPECT: /PASS/
- E3 endpoint series por puerto en GET /api/routers/{id} (o /ports/{id}/series)
- E4 commit Closes #302

## HOJA F — #299 health score por puerto (tras E+D)

- F1 score por puerto: saturación (rate/speed), errores delta, flapping,
  degradado; test determinista
- F2 expuesto en view-model (ports[].health) — commit Closes #299

## HOJA G — #300 LLDP ground truth

- G1 enlaces infra puerto↔puerto desde LLDP (chassis-id + port id) como
  verdad preferente sobre FDB inferido; test con fixture LLDP
  CHECK: go test ./internal/adapters/ -run Lldp -v | tail -3
  EXPECT: /PASS/
- G2 view-model: DistributionNode/Device con link exacto; commit Closes #300

## HOJA H — #301+#304 fingerprinting + device-type

- H1 clasificador determinista: DHCP fingerprint + OUI + LLDP caps +
  hostname → DeviceType (camera/phone/printer/ap/iot)
  CHECK: go test ./internal/adapters/ -run Finger|DeviceType -v | tail -3
  EXPECT: /PASS/
- H2 deviceType en view-model + chips/filtro mínimo en UI
- H3 commit Closes #301, Closes #304

## HOJA I — #306 front panel switches

- I1 PortPanel reutilizado para switches gestionados (distribution nodes
  con datos scraper/beacon o switch OpenWrt = router existente ya cubierto)
- I2 decisión documentada en el issue si queda parte para #309 (SNMP)

## NOTAS

- Cada hoja: worktree /tmp/opencode/np-<X>, rama sweep/<X> sobre origin/main
  del momento, commits SIN push; el parent integra, pushea PRs y mergea.
- PoE overload (#303): fuera de alcance hasta que exista dato PoE (#309/DDR);
  se deja nota en el issue.
- Reglas known: tsc real = npm run build; gate-check.mjs con --timeout N FILE.
