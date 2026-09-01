# Gates - Ajustes UI /devices (#437/#439)

- [x] G1: Eliminado del sheet el título/descripción "Renombrar dispositivo" y el campo de edición de nombre; solo queda icono, reserva y bloqueo.
  CHECK: ! grep -q "device-edit-name" app/src/components/DeviceEditSheet.tsx && echo clean
  EXPECT: clean
  EVIDENCE: clean

- [x] G2: Cambiado el guión (—) de la columna "Tiempo restante" por el texto "ilimitado" (i18n).
  CHECK: grep "leaseUnlimited" app/src/pages/Devices.tsx app/public/locales/es/translation.json app/public/locales/en/translation.json
  EXPECT: leaseUnlimited
  EVIDENCE: app/public/locales/es/translation.json:    "leaseUnlimited": "ilimitado", | app/public/locales/en/translation.json:    "leaseUnlimited": "unlimited",

- [x] G3: Build frontend sin errores.
  CHECK: cd app && npm run build >/tmp/build.log 2>&1 && grep "built in" /tmp/build.log
  EXPECT: built in
  EVIDENCE: ✓ built in 493ms | ✓ built in 17ms

- [x] G4: Lint frontend sin errores nuevos.
  CHECK: cd app && npm run lint 2>&1 | grep "0 errors"
  EXPECT: 0 errors
  EVIDENCE: ✖ 5 problems (0 errors, 5 warnings)

- [x] G5: Deploy en CT 226 con health OK.
  CHECK: curl -fsS http://192.168.1.226:3000/api/health
  EXPECT: "ok":true
  EVIDENCE: {"agentsConnected":4,"db":"ok","devicesTotal":88,"mode":"live","ok":true,"sseConnections":1,"uptimeSec":249,"version":"2.25.0-437"}

- [x] G6: Captura Playwright del sheet sin título de renombrar y con solo icono + reserva + bloqueo.
  CHECK: ls /tmp/device-sheet.png && echo screenshot-ok
  EXPECT: screenshot-ok
  EVIDENCE: /tmp/device-sheet.png | screenshot-ok
