// ssidColor: color pastel determinista por SSID (hash), como el channel
// analysis del LuCI: cada red vecina su color para reconocerla en la cascada
// y en la tabla. Compartido por la pestaña survey de Roaming (#542) y el
// informe de canales (#1070).

export const SSID_PALETTE = [
  '#c084fc', '#f472b6', '#34d399', '#60a5fa', '#fbbf24',
  '#f87171', '#2dd4bf', '#a78bfa', '#facc15', '#fb923c',
]

export function ssidColor(s: string): string {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0
  return SSID_PALETTE[h % SSID_PALETTE.length] ?? '#a78bfa'
}
