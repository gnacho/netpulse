package alerts

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

const rulesKey = "alert-rules.v1"

type RuleCondition struct {
	Metric    string        `json:"metric"`
	Operator  string        `json:"operator"`
	Threshold float64       `json:"threshold"`
	Duration  time.Duration `json:"duration"`
}

type RuleScope struct {
	Type      string   `json:"type"`
	RouterIDs []string `json:"routerIds,omitempty"`
}

type Rule struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Category  string        `json:"category"`
	Enabled   bool          `json:"enabled"`
	Condition RuleCondition `json:"condition"`
	Scope     RuleScope     `json:"scope"`
	Severity  string        `json:"severity"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

func ListRules(d *db.DB) ([]Rule, error) {
	var raw string
	if err := d.QueryRow("SELECT value FROM kv WHERE key = ?", rulesKey).Scan(&raw); err != nil {
		return []Rule{}, nil
	}
	var rules []Rule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil, fmt.Errorf("unmarshal rules: %w", err)
	}
	return rules, nil
}

func GetRule(d *db.DB, id string) (*Rule, error) {
	rules, err := ListRules(d)
	if err != nil {
		return nil, err
	}
	for _, r := range rules {
		if r.ID == id {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("regla %s no encontrada", id)
}

func SaveRules(d *db.DB, rules []Rule) error {
	data, err := json.Marshal(rules)
	if err != nil {
		return fmt.Errorf("marshal rules: %w", err)
	}
	_, err = d.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		rulesKey, string(data))
	return err
}

func CreateRule(d *db.DB, r Rule) error {
	rules, err := ListRules(d)
	if err != nil {
		return err
	}
	if r.ID == "" {
		r.ID = fmt.Sprintf("rule-%d", time.Now().UnixNano())
	}
	r.CreatedAt = time.Now()
	r.UpdatedAt = r.CreatedAt
	rules = append([]Rule{r}, rules...)
	return SaveRules(d, rules)
}

func UpdateRule(d *db.DB, r Rule) error {
	rules, err := ListRules(d)
	if err != nil {
		return err
	}
	for i, existing := range rules {
		if existing.ID == r.ID {
			r.CreatedAt = existing.CreatedAt
			r.UpdatedAt = time.Now()
			rules[i] = r
			return SaveRules(d, rules)
		}
	}
	return fmt.Errorf("regla %s no encontrada", r.ID)
}

func DeleteRule(d *db.DB, id string) error {
	rules, err := ListRules(d)
	if err != nil {
		return err
	}
	for i, r := range rules {
		if r.ID == id {
			rules = append(rules[:i], rules[i+1:]...)
			return SaveRules(d, rules)
		}
	}
	return fmt.Errorf("regla %s no encontrada", id)
}
