// Package orchestr — motor de plan/apply para Fase 10 (escritura/orquestación).
// Genera planes (diff desired vs current → Ops), los persiste en SQLite,
// los envía al agente vía SSE y registra el resultado en auditoría.
package orchestr

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// Errores del ciclo de vida del plan.
var (
	// ErrPlanNotFound: no hay ningún plan con ese id (p. ej. el agente
	// reporta un plan de una base de datos que se recreó).
	ErrPlanNotFound = errors.New("plan_not_found")
	// ErrInvalidTransition: el plan no está en el estado del que parte la
	// transición pedida. No se hacen UPDATE a ciegas.
	ErrInvalidTransition = errors.New("invalid_plan_transition")
)

// Plan es un plan de orquestación: desired state → diff (Ops) → apply.
type Plan struct {
	ID        string          `json:"id"`
	RouterID  string          `json:"routerId"`
	Resource  string          `json:"resource"`
	Desired   json.RawMessage `json:"desired"`
	Diff      []executor.Op   `json:"diff"`
	Status    string          `json:"status"` // pending|applying|applied|failed|rolled_back
	CreatedBy string          `json:"createdBy"`
	CreatedAt int64           `json:"createdAt"`
	AppliedAt *int64          `json:"appliedAt,omitempty"`
	Result    *ApplyResult    `json:"result,omitempty"`
	// Method NO se persiste: es metadato de la respuesta del POST /api/plans
	// que indica el escenario detectado (apk|opkg|none|binary) para que el
	// frontend lo muestre. Vacío en GET /api/plans/{id}.
	Method string `json:"method,omitempty"`
}

// ApplyResult es lo que el agente reporta tras ejecutar el plan.
type ApplyResult = executor.ApplyResult

// Manager gestiona el ciclo de vida de los planes.
type Manager struct {
	db *db.DB
}

// New crea el manager.
func New(d *db.DB) *Manager {
	return &Manager{db: d}
}

// CreatePlan genera y persiste un plan a partir del estado deseado.
// diffFn es la función específica del módulo (AdGuard, WireGuard, ...)
// que compara desired vs current y devuelve la lista de Ops.
func (m *Manager) CreatePlan(routerID, resource string, desired json.RawMessage, diff []executor.Op, createdBy string) (*Plan, error) {
	id := newID()
	diffJSON, _ := json.Marshal(diff)
	now := time.Now().Unix()
	if _, err := m.db.Exec(
		`INSERT INTO orchestr_plans (id, router_id, resource, desired, diff, status, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, 'pending', ?, ?)`,
		id, routerID, resource, string(desired), string(diffJSON), createdBy, now); err != nil {
		return nil, fmt.Errorf("insert plan: %w", err)
	}
	m.audit(id, "plan", createdBy, fmt.Sprintf("resource=%s ops=%d", resource, len(diff)))
	return m.GetPlan(id)
}

// GetPlan carga un plan por ID.
func (m *Manager) GetPlan(id string) (*Plan, error) {
	var p Plan
	var desired, diffJSON, status string
	var createdBy string
	var createdAt int64
	var appliedAt *int64
	var resultJSON *string

	err := m.db.QueryRow(
		`SELECT id, router_id, resource, desired, diff, status, created_by, created_at, applied_at, result
		 FROM orchestr_plans WHERE id = ?`, id).Scan(
		&p.ID, &p.RouterID, &p.Resource, &desired, &diffJSON, &status,
		&createdBy, &createdAt, &appliedAt, &resultJSON)
	if err != nil {
		return nil, err
	}
	// desired vacío en BD → nil (json.RawMessage("") revienta al serializar
	// con toolchains recientes: MarshalJSON de un documento vacío es error y
	// el POST /api/plans respondía 201 sin body).
	if desired != "" {
		p.Desired = json.RawMessage(desired)
	}
	json.Unmarshal([]byte(diffJSON), &p.Diff)
	p.Status = status
	p.CreatedBy = createdBy
	p.CreatedAt = createdAt
	p.AppliedAt = appliedAt
	if resultJSON != nil {
		var r ApplyResult
		json.Unmarshal([]byte(*resultJSON), &r)
		p.Result = &r
	}
	return &p, nil
}

// SetApplying marca el plan como en ejecución (pending → applying).
// Devuelve ErrInvalidTransition si el plan ya no está pendiente, de modo que
// un apply repetido o fuera de orden no puede mover el estado a ciegas.
func (m *Manager) SetApplying(id, actor string) error {
	return m.transition(id, "pending", "applying", "applying", actor, "apply requested")
}

// SetRollingBack marca el plan como revertido en curso (applied →
// rolling_back) porque un admin disparó POST /api/plans/{id}/rollback.
// Solo se acepta desde applied: revertir algo que no se aplicó no tiene
// sentido y dejaría el plan colgado. Registra auditoría.
func (m *Manager) SetRollingBack(id, actor string) error {
	return m.transition(id, "applied", "rolling_back", "rollback", actor, "manual rollback triggered")
}

// RestoreTransition deshace una transición transitoria cuando el envío al
// agente no llegó a entregarse (p. ej. applying → pending si el SSE falló).
// El UPDATE va condicionado al estado de partida, así que nunca pisa un
// resultado que el agente ya haya reportado. Registra auditoría "restore".
func (m *Manager) RestoreTransition(id, from, to, actor, reason string) error {
	switch {
	case from != "applying" && from != "rolling_back":
		return fmt.Errorf("%w: no se restaura desde %q", ErrInvalidTransition, from)
	case to != "pending" && to != "applied":
		return fmt.Errorf("%w: no se restaura a %q", ErrInvalidTransition, to)
	}
	detail := from + " -> " + to
	if reason != "" {
		detail += ": " + reason
	}
	return m.transition(id, from, to, "restore", actor, detail)
}

// transition mueve el plan de `from` a `to` solo si sigue en `from`
// (UPDATE condicional, nunca a ciegas) y deja una línea de auditoría.
func (m *Manager) transition(id, from, to, action, actor, detail string) error {
	res, err := m.db.Exec(`UPDATE orchestr_plans SET status = ? WHERE id = ? AND status = ?`, to, id, from)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var cur string
		switch err := m.db.QueryRow(`SELECT status FROM orchestr_plans WHERE id = ?`, id).Scan(&cur); {
		case errors.Is(err, sql.ErrNoRows):
			return ErrPlanNotFound
		case err != nil:
			return err
		}
		return fmt.Errorf("%w: estado %q, se esperaba %q", ErrInvalidTransition, cur, from)
	}
	m.audit(id, action, actor, detail)
	return nil
}

// SetResult actualiza el plan con el resultado del agente y registra auditoría.
// El estado final depende del estado de partida: si el plan estaba en
// 'rolling_back', el apply con las ops inversas se traduce a rolled_back;
// si el rollback en sí falló, queda failed. Nunca deja rolling_back.
func (m *Manager) SetResult(id string, res ApplyResult) error {
	var prev string
	if err := m.db.QueryRow(`SELECT status FROM orchestr_plans WHERE id = ?`, id).Scan(&prev); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPlanNotFound
		}
		return err
	}
	if res.Status == "" {
		res.Status = "failed"
		if res.Error == "" {
			res.Error = "el agente no reportó estado"
		}
	}
	final := res.Status
	switch prev {
	case "rolled_back":
		// rolled_back es terminal: un apply-result duplicado o tardío no
		// puede resucitar un plan que ya se revirtió.
		final = "rolled_back"
	case "rolling_back":
		switch res.Status {
		case "applied", "rolled_back":
			final = "rolled_back"
		case "failed":
			final = "failed" // el propio rollback no pudo revertir
		}
	case "applying":
		if res.Status == "rolled_back" {
			final = "rolled_back"
		}
	}
	resJSON, _ := json.Marshal(res)
	now := time.Now().Unix()
	_, err := m.db.Exec(
		`UPDATE orchestr_plans SET status = ?, applied_at = ?, result = ? WHERE id = ?`,
		final, now, string(resJSON), id)
	if err != nil {
		return err
	}
	m.audit(id, final, "agent", res.Error)
	return nil
}

// RecentAudit devuelve los últimos N eventos de auditoría.
func (m *Manager) RecentAudit(limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := m.db.Query(
		`SELECT plan_id, action, actor, detail, ts FROM orchestr_audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.PlanID, &e.Action, &e.Actor, &e.Detail, &e.Ts); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// AuditEntry es una línea del log de auditoría (append-only).
type AuditEntry struct {
	PlanID string `json:"planId"`
	Action string `json:"action"`
	Actor  string `json:"actor"`
	Detail string `json:"detail"`
	Ts     int64  `json:"ts"`
}

func (m *Manager) audit(planID, action, actor, detail string) {
	m.db.Exec(
		`INSERT INTO orchestr_audit (plan_id, action, actor, detail, ts) VALUES (?, ?, ?, ?, ?)`,
		planID, action, actor, detail, time.Now().Unix())
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
