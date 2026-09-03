// Package orchestr — motor de plan/apply para Fase 10 (escritura/orquestación).
// Genera planes (diff desired vs current → Ops), los persiste en SQLite,
// los envía al agente vía SSE y registra el resultado en auditoría.
package orchestr

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// Plan es un plan de orquestación: desired state → diff (Ops) → apply.
type Plan struct {
	ID        string             `json:"id"`
	RouterID  string             `json:"routerId"`
	Resource  string             `json:"resource"`
	Desired   json.RawMessage    `json:"desired"`
	Diff      []executor.Op      `json:"diff"`
	Status    string             `json:"status"` // pending|applying|applied|failed|rolled_back
	CreatedBy string             `json:"createdBy"`
	CreatedAt int64              `json:"createdAt"`
	AppliedAt *int64             `json:"appliedAt,omitempty"`
	Result    *ApplyResult       `json:"result,omitempty"`
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

// SetApplying marca el plan como en ejecución.
func (m *Manager) SetApplying(id string) error {
	_, err := m.db.Exec(`UPDATE orchestr_plans SET status = 'applying' WHERE id = ?`, id)
	return err
}

// SetRollingBack marca el plan como en revertido en curso (un admin disparó
// POST /api/plans/{id}/rollback). Registra auditoría.
func (m *Manager) SetRollingBack(id, actor string) error {
	_, err := m.db.Exec(`UPDATE orchestr_plans SET status = 'rolling_back' WHERE id = ?`, id)
	if err != nil {
		return err
	}
	m.audit(id, "rollback", actor, "manual rollback triggered")
	return nil
}

// SetResult actualiza el plan con el resultado del agente y registra auditoría.
// Si el plan estaba en 'rolling_back', traduce el resultado del agente al
// estado semántico del plan (applied → rolled_back, fallido → failed).
func (m *Manager) SetResult(id string, res ApplyResult) error {
	var prev string
	if err := m.db.QueryRow(`SELECT status FROM orchestr_plans WHERE id = ?`, id).Scan(&prev); err != nil {
		return err
	}
	final := res.Status
	if prev == "rolling_back" {
		switch res.Status {
		case "applied":
			final = "rolled_back"
		case "rolled_back":
			final = "failed" // el propio rollback no pudo revertir
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
