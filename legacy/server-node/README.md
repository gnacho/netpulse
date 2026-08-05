# legacy/server-node — backend Node (HISTÓRICO)

**Estado: histórico desde el 5-Ago-2026.** El backend activo es
[`server-go/`](../../server-go/) (reescritura en Go con paridad funcional
verificada). Este backend Node se conserva **solo como histórico/referencia** —
no se despliega ni se actualiza (decisión del usuario 5-Ago-2026: Go estable,
fuera de los nuevos deploys).

No recibe features nuevas ni fixes.

## Referencia (lo que fue el flujo de rollback)

```bash
systemctl disable --now netpulse-go.service
systemctl enable --now netpulse.service
```

- El Node arrancaba sobre el mismo `DATA_DIR` (`/opt/netpulse/server/data`) — la
  BD compartida era compatible en ambos sentidos (verificado el 1-Ago-2026:
  Node arrancando contra la BD ya migrada por Go: `/api/health` OK, login OK).
- Si la BD estuviera corrupta o incompatible: restaurar
  `/opt/netpulse/server/data/netpulse.db.bak-<timestamp>` (backup automático
  de la migración Node→Go).
- El updater en modo Node usa el flujo legacy de `deploy/update.sh`
  (dist precompilado de la prerelease `dist-latest`); el flag de reinicio
  vuelve a ser `/opt/netpulse/server/data/.restart-me` (unidad
  `netpulse-restart.path`, que se conserva activa en el CT).
