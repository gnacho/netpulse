// rack-catalog-check.mjs - verificación ejecutable del catálogo de faceplates
// (gates A1-A3). Corre con Node >= 23 (type stripping), sin dependencias.

const mod = await import('../src/lib/rackFaceplates.ts')
const { FACEPLATES, seedPorts, suggestFaceplate, applyFaceplate, getFaceplate } = mod

let failed = false
const bad = (msg) => {
  failed = true
  console.error('FAIL:', msg)
}

// A1: >= 9 plantillas, bandas no solapadas, puertos dentro de 0..1.
if (FACEPLATES.length < 9) bad(`solo ${FACEPLATES.length} plantillas (min 9)`)
for (const p of FACEPLATES) {
  if (![3, 4, 6, 12].includes(p.colSpan)) bad(`${p.id}: colSpan ${p.colSpan} fuera de {3,4,6,12}`)
  if (p.uHeight < 1) bad(`${p.id}: uHeight ${p.uHeight}`)
  // r = semi-extensión vertical (fracción del alto): validamos el eje Y y
  // que el centro X quede a la izquierda de la labelBox (banda fija LED,
  // luego labelBox: nunca se tocan).
  if (p.led.y - p.led.r < 0 || p.led.y + p.led.r > 1) bad(`${p.id}: led fuera del eje Y`)
  if (p.led.x < 0 || p.led.x > 1) bad(`${p.id}: led fuera del eje X`)
  const lb = p.labelBox
  if (lb.x < 0 || lb.y < 0 || lb.x + lb.w > 1 || lb.y + lb.h > 1) bad(`${p.id}: labelBox fuera de 0..1`)
  if (p.led.x >= lb.x) bad(`${p.id}: led debe quedar a la izquierda de la labelBox`)
  for (const row of p.rows) {
    if (row.count < 1) bad(`${p.id}: fila vacía`)
    if (row.xStart < 0 || row.xEnd > 1 || row.y < 0 || row.y > 1) bad(`${p.id}: fila fuera de 0..1`)
  }
}
const ids = new Set(FACEPLATES.map((p) => p.id))
if (ids.size !== FACEPLATES.length) bad('IDs de plantilla duplicados')
console.log(failed ? '' : 'CATALOG OK')

// A2: suggest para los tipos canónicos del clasificador.
const expected = [
  ['router', 'router'],
  ['ap', 'router'],
  ['switch', 'switch'],
  ['servidor', 'server-1u'],
  ['ordenador', 'server-1u'],
  ['nas', 'nas-tower'],
  ['ups', 'ups-4u'],
  ['pdu', 'pdu-1u'],
  ['desconocido', undefined],
  ['iot', undefined],
  ['', undefined],
]
for (const [typ, want] of expected) {
  const got = suggestFaceplate(typ)
  const gotId = got ? got.id : undefined
  if (gotId !== want) bad(`suggest("${typ}") = ${gotId}; want ${want}`)
}
console.log(failed ? '' : 'SUGGEST OK')

// A3: seed estable y applyFaceplate.
const sw = getFaceplate('switch')
const p1 = seedPorts(sw)
const p2 = seedPorts(sw)
if (JSON.stringify(p1) !== JSON.stringify(p2)) bad('seedPorts no es estable')
if (p1.length !== 28) bad(`switch: ${p1.length} puertos; want 28 (24 rj45 + 4 sfp+)`)
const kinds = p1.reduce((m, p) => ((m[p.kind] = (m[p.kind] || 0) + 1), m), {})
if (kinds.rj45 !== 24 || kinds['sfp+'] !== 4) bad(`switch kinds: ${JSON.stringify(kinds)}`)
const idsStable = p1.map((p) => p.id).join(',')
if (idsStable !== p1.map((_, i) => 'p' + String(i + 1).padStart(2, '0')).join(',')) bad('IDs de puerto inestables')
for (const p of p1) {
  if (p.x < 0 || p.x > 1 || p.y < 0 || p.y > 1) bad(`puerto ${p.id} fuera de 0..1`)
}
const applied = applyFaceplate('switch')
if (!applied || applied.plate.id !== 'switch' || applied.ports.length !== 28) bad('applyFaceplate(switch) roto')
// A4b: layoutPhysicalPorts determinista y con ids reales preservados.
const { layoutPhysicalPorts } = mod
const phys = layoutPhysicalPorts([
  ...Array.from({ length: 5 }, (_, i) => ({ id: 'lan' + (i + 1), kind: 'rj45' })),
  { id: 'eth1', kind: 'sfp+' },
])
if (phys.length !== 6) bad(`layoutPhysicalPorts: ${phys.length}; want 6`)
if (phys[5].id !== 'eth1' || phys[5].x < 0.7) bad('sfp+ no queda a la derecha')
if (phys[0].id !== 'lan1') bad('ids reales no preservados')
for (const p of phys) if (p.x < 0 || p.x > 1 || p.y < 0 || p.y > 1) bad(`puerto físico ${p.id} fuera de 0..1`)
console.log(failed ? '' : 'LAYOUT OK')

const appliedBad = applyFaceplate('iot')
if (appliedBad !== undefined) bad('applyFaceplate(iot) debería ser undefined')
console.log(failed ? '' : 'SEED OK')

console.log(failed ? 'CHECK FAILED' : 'ALL CHECKS PASS')
process.exit(failed ? 1 : 0)
