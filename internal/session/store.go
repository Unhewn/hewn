// Package session is SQLite persistence for conversations: sessions,
// messages, and tool calls. Message and tool-call content are stored as
// opaque JSON -- this package has no dependency on internal/provider or
// internal/tool; the caller (internal/agent) marshals/unmarshals at the
// boundary.
package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Role is who a Message is attributed to.
type Role string

// Role values.
const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
	RoleSystem    Role = "system"
)

// Session is one recorded conversation.
type Session struct {
	ID        string
	ParentID  string // empty if none; forking unused in v0.1
	ForkPoint int64
	Title     string
	CWD       string
	Provider  string
	Model     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Usage carries token accounting for one message.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Message is one turn of a Session's history. Content is opaque JSON to
// this package.
type Message struct {
	ID        string
	SessionID string
	Seq       int
	Role      Role
	Content   json.RawMessage
	Usage     *Usage
	CreatedAt time.Time
}

// ToolCall is one tool invocation belonging to a Message.
type ToolCall struct {
	ID         string
	MessageID  string
	Tool       string
	Params     json.RawMessage
	Result     string
	IsError    bool
	Approved   *int // 0 denied, 1 once, 2 session-wide; nil = not gated
	DurationMS int64
	CreatedAt  time.Time
}

// Store is the SQLite-backed persistence layer.
type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) the database at path, creating its
// parent directory as needed, and applies any pending migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("session: create db directory %s: %w", dir, err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("session: open %s: %w", path, err)
	}

	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

const maxTitleRunes = 60

func truncateTitle(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= maxTitleRunes {
		return s
	}
	return string(runes[:maxTitleRunes])
}

// CreateSession inserts a new session, deriving its title from the first
// ~60 runes of titleSource (typically the initial prompt) -- no LLM
// summarization, just a useful label for --list.
func (s *Store) CreateSession(ctx context.Context, cwd, provider, model, titleSource string) (Session, error) {
	now := time.Now()
	sess := Session{
		ID:        New(),
		Title:     truncateTitle(titleSource),
		CWD:       cwd,
		Provider:  provider,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, title, cwd, provider, model, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.Title, sess.CWD, sess.Provider, sess.Model, now.UnixMilli(), now.UnixMilli(),
	)
	if err != nil {
		return Session{}, fmt.Errorf("session: create session: %w", err)
	}
	return sess, nil
}

// Touch bumps a session's updated_at to now, e.g. after appending a
// message.
func (s *Store) Touch(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE id = ?`, time.Now().UnixMilli(), sessionID)
	if err != nil {
		return fmt.Errorf("session: touch session %s: %w", sessionID, err)
	}
	return nil
}

// AppendMessage inserts the next message in a session's history, assigning
// seq as one past the current maximum for that session inside a single
// transaction (AGENTS.md: explicit transactions for multi-statement
// writes). usage may be nil (not yet known, e.g. a user message).
func (s *Store) AppendMessage(ctx context.Context, sessionID string, role Role, content json.RawMessage, usage *Usage) (Message, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, fmt.Errorf("session: begin append message: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed; the error path already reports the real failure

	var seq int
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM messages WHERE session_id = ?`, sessionID).Scan(&seq)
	if err != nil {
		return Message{}, fmt.Errorf("session: assign message seq: %w", err)
	}

	now := time.Now()
	msg := Message{
		ID:        New(),
		SessionID: sessionID,
		Seq:       seq,
		Role:      role,
		Content:   content,
		Usage:     usage,
		CreatedAt: now,
	}

	var inputTokens, outputTokens sql.NullInt64
	if usage != nil {
		inputTokens = sql.NullInt64{Int64: int64(usage.InputTokens), Valid: true}
		outputTokens = sql.NullInt64{Int64: int64(usage.OutputTokens), Valid: true}
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO messages (id, session_id, seq, role, content, input_tokens, output_tokens, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, sessionID, seq, string(role), string(content), inputTokens, outputTokens, now.UnixMilli(),
	)
	if err != nil {
		return Message{}, fmt.Errorf("session: append message: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Message{}, fmt.Errorf("session: commit append message: %w", err)
	}
	return msg, nil
}

// AppendToolCall inserts one tool call belonging to messageID. approved is
// nil for calls that were never gated (RiskReadOnly tools); otherwise 0/1/2
// matching tool.Decision's Deny/AllowOnce/AllowSession values.
func (s *Store) AppendToolCall(
	ctx context.Context,
	messageID, toolName string,
	params json.RawMessage,
	result string,
	isError bool,
	approved *int,
	duration time.Duration,
) (ToolCall, error) {
	now := time.Now()
	tc := ToolCall{
		ID:         New(),
		MessageID:  messageID,
		Tool:       toolName,
		Params:     params,
		Result:     result,
		IsError:    isError,
		Approved:   approved,
		DurationMS: duration.Milliseconds(),
		CreatedAt:  now,
	}

	var approvedVal sql.NullInt64
	if approved != nil {
		approvedVal = sql.NullInt64{Int64: int64(*approved), Valid: true}
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tool_calls (id, message_id, tool, params, result, is_error, approved, duration_ms, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tc.ID, messageID, toolName, string(params), result, isError, approvedVal, tc.DurationMS, now.UnixMilli(),
	)
	if err != nil {
		return ToolCall{}, fmt.Errorf("session: append tool call: %w", err)
	}
	return tc, nil
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanSession(sc scanner) (Session, error) {
	var (
		sess                 Session
		parentID             sql.NullString
		forkPoint            sql.NullInt64
		title                sql.NullString
		createdAt, updatedAt int64
	)

	err := sc.Scan(&sess.ID, &parentID, &forkPoint, &title, &sess.CWD, &sess.Provider, &sess.Model, &createdAt, &updatedAt)
	if err != nil {
		return Session{}, err
	}

	sess.ParentID = parentID.String
	sess.ForkPoint = forkPoint.Int64
	sess.Title = title.String
	sess.CreatedAt = time.UnixMilli(createdAt)
	sess.UpdatedAt = time.UnixMilli(updatedAt)
	return sess, nil
}

const sessionColumns = `id, parent_id, fork_point, title, cwd, provider, model, created_at, updated_at`

// LoadSession looks up a session by exact ID, by a unique ID prefix, or --
// when idOrPrefix is empty -- the most recently updated session.
func (s *Store) LoadSession(ctx context.Context, idOrPrefix string) (Session, error) {
	var row *sql.Row
	if idOrPrefix == "" {
		row = s.db.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions ORDER BY updated_at DESC LIMIT 1`)
	} else {
		row = s.db.QueryRowContext(ctx,
			`SELECT `+sessionColumns+` FROM sessions WHERE id = ? OR id LIKE ? ORDER BY (id = ?) DESC, updated_at DESC LIMIT 1`,
			idOrPrefix, idOrPrefix+"%", idOrPrefix,
		)
	}

	sess, err := scanSession(row)
	if err != nil {
		return Session{}, fmt.Errorf("session: load session %q: %w", idOrPrefix, err)
	}
	return sess, nil
}

// ListSessions returns up to limit sessions, most recently updated first.
func (s *Store) ListSessions(ctx context.Context, limit int) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sessionColumns+` FROM sessions ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("session: list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("session: scan session: %w", err)
		}
		sessions = append(sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session: iterate sessions: %w", err)
	}
	return sessions, nil
}

// LoadMessages returns a session's messages in seq order, for replaying
// into Loop.history on --resume.
func (s *Store) LoadMessages(ctx context.Context, sessionID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, seq, role, content, input_tokens, output_tokens, created_at
		 FROM messages WHERE session_id = ? ORDER BY seq`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("session: load messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var (
			msg                       Message
			role, content             string
			inputTokens, outputTokens sql.NullInt64
			createdAt                 int64
		)
		if err := rows.Scan(&msg.ID, &msg.SessionID, &msg.Seq, &role, &content, &inputTokens, &outputTokens, &createdAt); err != nil {
			return nil, fmt.Errorf("session: scan message: %w", err)
		}

		msg.Role = Role(role)
		msg.Content = json.RawMessage(content)
		msg.CreatedAt = time.UnixMilli(createdAt)
		if inputTokens.Valid {
			msg.Usage = &Usage{InputTokens: int(inputTokens.Int64), OutputTokens: int(outputTokens.Int64)}
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session: iterate messages: %w", err)
	}
	return messages, nil
}

// LoadToolCalls returns the tool calls belonging to a message, in the
// order they were recorded.
func (s *Store) LoadToolCalls(ctx context.Context, messageID string) ([]ToolCall, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, message_id, tool, params, result, is_error, approved, duration_ms, created_at
		 FROM tool_calls WHERE message_id = ? ORDER BY created_at`,
		messageID,
	)
	if err != nil {
		return nil, fmt.Errorf("session: load tool calls: %w", err)
	}
	defer rows.Close()

	var calls []ToolCall
	for rows.Next() {
		var (
			tc        ToolCall
			params    string
			result    sql.NullString
			approved  sql.NullInt64
			createdAt int64
		)
		err := rows.Scan(&tc.ID, &tc.MessageID, &tc.Tool, &params, &result, &tc.IsError, &approved, &tc.DurationMS, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("session: scan tool call: %w", err)
		}

		tc.Params = json.RawMessage(params)
		tc.Result = result.String
		tc.CreatedAt = time.UnixMilli(createdAt)
		if approved.Valid {
			v := int(approved.Int64)
			tc.Approved = &v
		}
		calls = append(calls, tc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session: iterate tool calls: %w", err)
	}
	return calls, nil
}
