package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"inner-companion/internal/protocol"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store using a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) a SQLite database at the given path.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable WAL mode and foreign keys
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("exec %s: %w", pragma, err)
		}
	}

	if err := createTables(db); err != nil {
		db.Close()
		return nil, err
	}

	return &SQLiteStore{db: db}, nil
}

func createTables(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			session_id     TEXT PRIMARY KEY,
			agent_id       TEXT NOT NULL,
			memory_summary TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS messages (
			seq        INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL REFERENCES sessions(session_id),
			role       TEXT NOT NULL,
			content    TEXT NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("create tables: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CreateSession(ctx context.Context, sessionID, agentID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO sessions (session_id, agent_id) VALUES (?, ?)`,
		sessionID, agentID,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) LoadHistory(ctx context.Context, sessionID string) ([]protocol.HistoryMessage, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT role, content FROM messages WHERE session_id = ? ORDER BY seq ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("load history: %w", err)
	}
	defer rows.Close()

	var msgs []protocol.HistoryMessage
	for rows.Next() {
		var role, contentJSON string
		if err := rows.Scan(&role, &contentJSON); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		var blocks []protocol.HistoryContentBlock
		if err := json.Unmarshal([]byte(contentJSON), &blocks); err != nil {
			return nil, fmt.Errorf("unmarshal content: %w", err)
		}
		msgs = append(msgs, protocol.HistoryMessage{Role: role, Content: blocks})
	}
	return msgs, rows.Err()
}

func (s *SQLiteStore) AppendMessages(ctx context.Context, sessionID string, msgs []protocol.HistoryMessage) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO messages (session_id, role, content) VALUES (?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for _, m := range msgs {
		contentJSON, err := json.Marshal(m.Content)
		if err != nil {
			return fmt.Errorf("marshal content: %w", err)
		}
		if _, err := stmt.ExecContext(ctx, sessionID, m.Role, string(contentJSON)); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) LoadMeta(ctx context.Context, sessionID string) (SessionMeta, error) {
	var meta SessionMeta
	err := s.db.QueryRowContext(ctx,
		`SELECT session_id, agent_id, memory_summary FROM sessions WHERE session_id = ?`,
		sessionID,
	).Scan(&meta.SessionID, &meta.AgentID, &meta.MemorySummary)
	if err != nil {
		return SessionMeta{}, fmt.Errorf("load meta: %w", err)
	}
	return meta, nil
}

func (s *SQLiteStore) SaveMemorySummary(ctx context.Context, sessionID, summary string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET memory_summary = ? WHERE session_id = ?`,
		summary, sessionID,
	)
	if err != nil {
		return fmt.Errorf("save memory summary: %w", err)
	}
	return nil
}

func (s *SQLiteStore) MessageCount(ctx context.Context, sessionID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE session_id = ?`,
		sessionID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("message count: %w", err)
	}
	return count, nil
}

func (s *SQLiteStore) MaxSeq(ctx context.Context, sessionID string) (int64, error) {
	var maxSeq sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM messages WHERE session_id = ?`,
		sessionID,
	).Scan(&maxSeq)
	if err != nil {
		return 0, fmt.Errorf("max seq: %w", err)
	}
	if !maxSeq.Valid {
		return 0, nil
	}
	return maxSeq.Int64, nil
}

func (s *SQLiteStore) DeleteMessagesBefore(ctx context.Context, sessionID string, seq int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM messages WHERE session_id = ? AND seq < ?`,
		sessionID, seq,
	)
	if err != nil {
		return fmt.Errorf("delete messages before: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
