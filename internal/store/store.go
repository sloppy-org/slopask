package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	_ "modernc.org/sqlite"
)

// Store wraps a SQLite database connection for slopask.
type Store struct {
	db *sql.DB
}

// Open creates or opens the SQLite database at the given path,
// enables WAL mode and foreign keys, and runs migrations.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dbPath := filepath.Join(dataDir, "slopask.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// OpenMemory creates an in-memory Store for testing.
// Each call returns an isolated database backed by a uniquely named
// shared-cache in-memory SQLite instance (allowing concurrent queries).
func OpenMemory() (*Store, error) {
	n := memorySeq.Add(1)
	dsn := fmt.Sprintf("file:mem%d?mode=memory&cache=shared&_pragma=foreign_keys(ON)", n)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open memory database: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping memory database: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

var memorySeq atomic.Int64

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	// Migration: if old answers table lacks version column, drop and recreate.
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('answers') WHERE name='version'").Scan(&count); err == nil && count == 0 {
		s.db.Exec("DROP TABLE IF EXISTS answer_media")
		s.db.Exec("DROP TABLE IF EXISTS answer_votes")
		s.db.Exec("DROP TABLE IF EXISTS answers")
	}

	// Migration: add original_body column to questions if missing.
	s.db.Exec("ALTER TABLE questions ADD COLUMN original_body TEXT NOT NULL DEFAULT ''")

	const schema = `
CREATE TABLE IF NOT EXISTS rooms (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  slug TEXT NOT NULL UNIQUE,
  admin_token TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  created_at INTEGER NOT NULL DEFAULT (unixepoch()),
  archived INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS questions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  room_id INTEGER NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
  body TEXT NOT NULL DEFAULT '',
  original_body TEXT NOT NULL DEFAULT '',
  voter_id TEXT NOT NULL DEFAULT '',
  vote_count INTEGER NOT NULL DEFAULT 0,
  answered INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE INDEX IF NOT EXISTS idx_questions_room ON questions(room_id, created_at);

CREATE TABLE IF NOT EXISTS question_media (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  question_id INTEGER NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  filename TEXT NOT NULL,
  disk_path TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  size_bytes INTEGER NOT NULL,
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS votes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  question_id INTEGER NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
  voter_id TEXT NOT NULL,
  created_at INTEGER NOT NULL DEFAULT (unixepoch()),
  UNIQUE(question_id, voter_id)
);

CREATE TABLE IF NOT EXISTS answers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  question_id INTEGER NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
  version INTEGER NOT NULL DEFAULT 1,
  body TEXT NOT NULL DEFAULT '',
  thumbs_up INTEGER NOT NULL DEFAULT 0,
  thumbs_down INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL DEFAULT (unixepoch()),
  UNIQUE(question_id, version)
);

CREATE TABLE IF NOT EXISTS answer_votes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  answer_id INTEGER NOT NULL REFERENCES answers(id) ON DELETE CASCADE,
  voter_id TEXT NOT NULL,
  direction INTEGER NOT NULL,
  created_at INTEGER NOT NULL DEFAULT (unixepoch()),
  UNIQUE(answer_id, voter_id)
);

CREATE TABLE IF NOT EXISTS answer_media (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  answer_id INTEGER NOT NULL REFERENCES answers(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  filename TEXT NOT NULL,
  disk_path TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  size_bytes INTEGER NOT NULL,
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);`
	_, err := s.db.Exec(schema)
	return err
}
