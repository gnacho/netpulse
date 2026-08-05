import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * Salir del modo demo (frontend estático sin backend): aquí no hay sesión ni
 * login al que volver, así que se intenta cerrar la ventana; si el navegador
 * bloquea window.close() (pestañas no abiertas por script), se vuelve a la
 * landing pública de NetPulse.
 */
export function exitDemo() {
  try {
    sessionStorage.removeItem('netpulse-demo')
  } catch {
    /* modo privado */
  }
  window.close()
  window.setTimeout(() => {
    window.location.href = 'https://netpulse.cloudless.club/'
  }, 300)
}
