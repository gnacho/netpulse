// webhook_admin.go — GET /api/webhook/dlq: eventos de webhook no entregados
// (DLQ de Fase 8.7b). Solo admin. Para diagnóstico de envíos fallidos.
package httpapi

import (
	"net/http"
)

// dlqEntry es el shape devuelto (payload no se expone entero, solo metadatos).
type dlqEntry struct {
	EventID string `json:"eventId"`
	SentAt  int64  `json:"sentAt"`
	Error   string `json:"error,omitempty"`
}

func (s *server) handleWebhookDLQ(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(
		"SELECT event_id, sent_at, error FROM webhook_events ORDER BY sent_at DESC LIMIT 50")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	defer rows.Close()
	out := []dlqEntry{}
	for rows.Next() {
		var e dlqEntry
		var sentAt int64
		var errNull *string
		if err := rows.Scan(&e.EventID, &sentAt, &errNull); err != nil {
			continue
		}
		e.SentAt = sentAt
		if errNull != nil {
			e.Error = *errNull
		}
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": len(out)})
}
