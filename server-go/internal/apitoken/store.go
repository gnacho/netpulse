package apitoken

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

const (
	TokenPrefix = "np_"
	ScopeRead   = "read"
	ScopeWrite  = "write"
	ScopeAdmin  = "admin"
)

type Token struct {
	ID         string
	Name       string
	Scope      string
	UserID     int64
	TokenHash  string
	CreatedAt  int64
	ExpiresAt  int64
	LastUsedAt int64
}

func hashToken(secret, raw string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(raw))
	return hex.EncodeToString(m.Sum(nil))
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func newID() string    { return randomHex(16) }
func newRaw() string    { return randomHex(24) }

type Store struct {
	db     *db.DB
	secret string
}

func NewStore(d *db.DB, secret string) *Store {
	return &Store{db: d, secret: secret}
}

func (s *Store) Create(name, scope string, userID int64, expiresInDays int) (id string, raw string, err error) {
	if name == "" || scope == "" {
		return "", "", fmt.Errorf("name and scope required")
	}
	if scope != ScopeRead && scope != ScopeWrite && scope != ScopeAdmin {
		return "", "", fmt.Errorf("invalid scope: %s", scope)
	}
	id = newID()
	raw = TokenPrefix + newRaw()
	h := hashToken(s.secret, raw)
	now := db.NowMS()
	var exp int64
	if expiresInDays > 0 {
		exp = now + int64(expiresInDays)*24*60*60*1000
	}
	_, err = s.db.Exec(
		`INSERT INTO api_tokens (id, name, scope, user_id, token_hash, created_at, expires_at, last_used_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
		id, name, scope, userID, h, now, exp,
	)
	return id, raw, err
}

func (s *Store) Get(id string) *Token {
	var t Token
	err := s.db.QueryRow(
		`SELECT id, name, scope, user_id, token_hash, created_at, expires_at, last_used_at
		 FROM api_tokens WHERE id = ?`, id,
	).Scan(&t.ID, &t.Name, &t.Scope, &t.UserID, &t.TokenHash, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt)
	if err != nil {
		return nil
	}
	return &t
}

func (s *Store) Validate(raw string) *Token {
	if !strings.HasPrefix(raw, TokenPrefix) {
		return nil
	}
	h := hashToken(s.secret, raw)
	var t Token
	err := s.db.QueryRow(
		`SELECT id, name, scope, user_id, token_hash, created_at, expires_at, last_used_at
		 FROM api_tokens WHERE token_hash = ?`, h,
	).Scan(&t.ID, &t.Name, &t.Scope, &t.UserID, &t.TokenHash, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt)
	if err != nil {
		return nil
	}
	if t.ExpiresAt > 0 && t.ExpiresAt < db.NowMS() {
		return nil
	}
	now := db.NowMS()
	if now-t.LastUsedAt > 60000 {
		_, _ = s.db.Exec("UPDATE api_tokens SET last_used_at = ? WHERE id = ?", now, t.ID)
	}
	return &t
}

// ValidateBearer implements auth.TokenValidator.
func (s *Store) ValidateBearer(raw string) (*auth.User, string) {
	t := s.Validate(raw)
	if t == nil {
		return nil, ""
	}
	u := auth.GetUserByID(s.db, t.UserID)
	if u == nil {
		return nil, ""
	}
	if t.Scope == ScopeAdmin && u.Role != "admin" {
		return nil, ""
	}
	return u, t.Scope
}

func (s *Store) List(userID int64, admin bool) ([]Token, error) {
	var rows *sql.Rows
	var err error
	if admin {
		rows, err = s.db.Query(
			`SELECT id, name, scope, user_id, token_hash, created_at, expires_at, last_used_at
			 FROM api_tokens ORDER BY created_at DESC`,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, name, scope, user_id, token_hash, created_at, expires_at, last_used_at
			 FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`,
			userID,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.ID, &t.Name, &t.Scope, &t.UserID, &t.TokenHash, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) Delete(id string, userID int64, admin bool) error {
	if admin {
		_, err := s.db.Exec("DELETE FROM api_tokens WHERE id = ?", id)
		return err
	}
	_, err := s.db.Exec("DELETE FROM api_tokens WHERE id = ? AND user_id = ?", id, userID)
	return err
}

func (s *Store) Count() (int, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM api_tokens").Scan(&n)
	return n, err
}

func EnsureSchema(d *db.DB) error {
	_, err := d.Exec(`
		CREATE TABLE IF NOT EXISTS api_tokens (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			scope TEXT NOT NULL,
			user_id INTEGER NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL DEFAULT 0,
			last_used_at INTEGER NOT NULL DEFAULT 0
		)
	`)
	if err != nil {
		return err
	}
	_, err = d.Exec("CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id)")
	return err
}
