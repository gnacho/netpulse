# Gates - Quitar override de nombre, leer nombre de OpenWrt (#437)

- [ ] G1: Tabla device_overrides ya no tiene columna name.
  CHECK: ! grep -R "device_overrides" server-go/internal/db/db.go | grep -q "name" && echo no-name-col
  EXPECT: no-name-col
  EVIDENCE: pending

- [ ] G2: Endpoint de overrides solo acepta/guarda icono.
  CHECK: grep -n "type DeviceOverride\|IconOverride\|NameOverride" server-go/internal/httpapi/device_overrides.go | grep -v "// "
  EXPECT: IconOverride
  EVIDENCE: pending

- [ ] G3: live.go no aplica override de nombre.
  CHECK: ! grep -n "deviceOverrides.*name\|override.*name\|overrideName" server-go/internal/adapters/live.go | grep -q "name" && echo no-name-override
  EXPECT: no-name-override
  EVIDENCE: pending

- [ ] G4: Frontend guarda solo icono, no pasa nombre.
  CHECK: grep -n "onSave.*null.*icon\|onSaveIcon\|name.*null" app/src/pages/Devices.tsx app/src/components/DeviceEditSheet.tsx
  EXPECT: null
  EVIDENCE: pending

- [ ] G5: go test ./... verde.
  CHECK: cd server-go && go test ./... 2>&1 | tail -5
  EXPECT: ok
  EVIDENCE: pending

- [ ] G6: npm build/lint OK.
  CHECK: cd app && npm run build >/tmp/build2.log 2>&1 && npm run lint 2>&1 | grep "0 errors" && grep "built in" /tmp/build2.log
  EXPECT: 0 errors
  EVIDENCE: pending

- [ ] G7: Deploy CT 226 con health OK.
  CHECK: curl -fsS http://192.168.1.226:3000/api/health
  EXPECT: "ok":true
  EVIDENCE: pending

- [ ] G8: Captura Playwright del sheet sin campo nombre y con icono/reserva/bloqueo.
  CHECK: node /tmp/opencode/pwtest/verify-sheet.mjs && ls /tmp/device-sheet.png && echo screenshot-ok
  EXPECT: screenshot-ok
  EVIDENCE: pending
