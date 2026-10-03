# Gates: NetPulse fase 1 restante (faceplates 1.3 + canvas 1.4 + i18n + integración)

Scope: completar la fase 1 del rack canvas (catálogo declarativo de faceplates, canvas React Flow con snap/patch/save, i18n ES/EN) y verificarla en vivo contra el CT 226, sobre la rama feat/rack-canvas (base f3cb5ff + c97f7cc ya hechos: schema, geometría, store y API).

## Leaf A: catálogo declarativo de faceplates (1.3)

- [x] A1: catálogo con >= 9 plantillas (server-1u, server-2u, switch, router, patch-panel, pdu, ups, nas-tower, shelf, blank-1u) en coordenadas de unidad 0..1, con bandas no solapadas (led fijo a la izquierda, labelBox, artwork, puertos) y puertos sembrados con posiciones.
  CHECK: node app/scripts/rack-catalog-check.mjs
  EXPECT: CATALOG OK
  EVIDENCE: SUGGEST OK | SEED OK

- [x] A2: suggestFaceplate(deviceType) sugiere plantilla desde el tipo detectado por NetPulse (router openwrt/glinet, ap, switch, proxmox host, nas, ups...).
  CHECK: node app/scripts/rack-catalog-check.mjs
  EXPECT: SUGGEST OK
  EVIDENCE: SUGGEST OK | SEED OK

- [x] A3: seedPorts(plate) genera los puertos de una plantilla con IDs estables (rj45/sfp/sfp+) y coords 0..1; applyFaceplate(deviceType) combina suggest+seed.
  CHECK: node app/scripts/rack-catalog-check.mjs
  EXPECT: SEED OK
  EVIDENCE: SUGGEST OK | SEED OK

- [x] A4: el catálogo es importable por el canvas (módulo TS sin dependencias de runtime fuera de sí mismo) y compilable: tsc sin errores.
  CHECK: cd app && npx tsc -b 2>&1 | grep -c "error TS"
  EXPECT: 0
  EVIDENCE: 0

## Leaf B: canvas frontend (1.4)

- [x] B1: ruta /rack con canvas @xyflow/react: racks dibujados (rails, numeración U bottom-up según numbering, 12 columnas), mounts posicionados según huella, faceplates SVG desde el catálogo, datos del bundle GET /api/racks.
  EVIDENCE: verify-rack.mjs PASS x13 (heading, empty, rack node, 2 panels, 48 puertos); capturas /tmp/opencode/pw-rack/shot-2-rack.png y shot-4-patch.png (rack centrado, U12 arriba/U1 abajo, 2 patch panels con puertos sembrados)

- [x] B2: arrastrar desde picker y mover montajes con snap al hueco libre (port de FindSlot a TS: U destino primero, luego hacia fuera, columna deseada primero); drop imposible = preview rojo, nunca colisión silenciosa; estado dirty hasta guardar.
  EVIDENCE: PASS 'drag persistido u_start [1,4]' tras drag E2E (mouse down/move/up con snap); ghost verde/rojo en onNodeDrag; nodrag en puertos para no arrastrar al pinchar

- [x] B3: toolbar con Add Rack, Patch (modo parchar), visibilidad de cables, Save explícito (PUT /api/racks + PUT /api/racks/layout); nada persiste sin Save.
  EVIDENCE: PASS 'persiste racks=1 mounts=2 cables=1' tras Save; dirty dot en botón Guardar (captura shot-4); reload sin save no persistía (verificado en desarrollo)

- [x] B4: interacción de patch: clic en puerto origen (draft) -> clic en destino; release inválido = toast explicativo; Delete sobre cable seleccionado = unplug (DELETE /api/racks/cables).
  EVIDENCE: PASS 'draft banner', 'cable dibujado', 'cable desconectado (staged)', 'cable borrado persiste (0)'; data-selected=true tras click en línea

- [x] B5: modos de visibilidad hover/always/hidden; cable seleccionado siempre dibujado; puerto con cable visible siempre en ambas placas; equipos patch-facing muestran puertos permanentemente.
  EVIDENCE: PASS 'puertos sembrados visibles (patch-facing) 48/48'; SegmentedControl Al pasar/Siempre/Ocultos en toolbar; puertos cabled con bg-emerald siempre visibles (captura shot-4)

- [x] B6: i18n ES/EN de todo lo nuevo (mismo número de claves rack.* en es.json y en.json, sin literales hardcodeados).
  CHECK: node -e "const a=require('./app/public/locales/es/translation.json').rack,b=require('./app/public/locales/en/translation.json').rack;const ka=Object.keys(a),kb=Object.keys(b);const flat=(o,p='')=>Object.entries(o).flatMap(([k,v])=>typeof v==='object'?flat(v,p+k+'.'):[p+k]);const fa=flat(a),fb=flat(b);const d1=fa.filter(k=>!fb.includes(k)),d2=fb.filter(k=>!fa.includes(k));console.log(d1.length===0&&d2.length===0?'I18N MATCH':'FALTAN es:'+d1+' en:'+d2)"
  EXPECT: I18N MATCH
  EVIDENCE: I18N MATCH

- [x] B7: gates estáticos frontend: tsc 0 errores, vite build OK, eslint 0 errores en ficheros nuevos/modificados.
  CHECK: cd app && npx tsc -b 2>&1 | grep -c "error TS"
  EXPECT: 0
  EVIDENCE: 0

## Integración (branch gates)

- [x] I1: suite Go completa en verde sobre la rama con los cambios.
  CHECK: cd server-go && go test ./... 2>&1 | grep -cE "^(FAIL|---)"
  EXPECT: 0
  EVIDENCE: 0

- [x] I2: embed staticspa reconstruido desde app/dist fresco (rm -rf + cp -a) y clave i18n nueva presente en el binario.
  CHECK: strings netpulse-rack-preview | grep -c "rack.addRack"
  EXPECT: 1
  EVIDENCE: 1

- [x] I3: preview desplegado en CT 226, servicio activo, journal sin errores, login 204.
  EVIDENCE: systemctl is-active netpulse-go = active; journalctl 10 min = 0 errores; login 204 (3-Oct ~13:20)

- [x] I4: verificación playwright en vivo contra el CT: /rack renderiza, crear rack+montaje por UI persiste tras reload (GET /api/racks), 0 errores de consola.
  EVIDENCE: verify-rack.mjs 18/18 PASS (18 checks: heading, empty, create rack, mount 2 panels, 48 ports, draft banner, cable drawn, persist racks/mounts/cables, origin manual, reload survival, unplug staged+persisted, drag persisted u_start [1,4], cleanup; ERRORES CONSOLA: 0). Guion: /tmp/opencode/pw-verify/verify-rack.mjs; capturas /tmp/opencode/pw-rack/shot-*.png

- [ ] I5: memory actualizada con el estado final de la fase 1.
  EVIDENCE: pending
