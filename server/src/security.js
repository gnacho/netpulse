/**
 * Headers de seguridad obligatorios (middleware global).
 * CSP: SPA con inline script de tema (ver app/index.html) → 'unsafe-inline'
 * en script-src/style-src, resto 'self'.
 */
export function securityHeaders() {
  return async (c, next) => {
    c.header('X-Content-Type-Options', 'nosniff')
    c.header('X-Frame-Options', 'DENY')
    c.header('Referrer-Policy', 'strict-origin-when-cross-origin')
    c.header('Permissions-Policy', 'geolocation=(), microphone=(), camera=()')
    c.header('Strict-Transport-Security', 'max-age=31536000; includeSubDomains')
    c.header(
      'Content-Security-Policy',
      "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; img-src 'self' data:; connect-src 'self'",
    )
    await next()
  }
}
