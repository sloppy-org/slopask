package store

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
)

// Room represents a Q&A room.
type Room struct {
	ID         int64  `json:"id"`
	Slug       string `json:"slug"`
	AdminToken string `json:"admin_token"`
	Title      string `json:"title"`
	CreatedAt  int64  `json:"created_at"`
	Archived   bool   `json:"archived"`
}

const (
	slugLen  = 12
	tokenLen = 24
)

var (
	ErrNotFound = errors.New("not found")
	slugChars   = []byte("abcdefghijklmnopqrstuvwxyz0123456789")
	tokenChars  = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
)

func randomString(charset []byte, length int) (string, error) {
	buf := make([]byte, length)
	for i := range buf {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", fmt.Errorf("random: %w", err)
		}
		buf[i] = charset[idx.Int64()]
	}
	return string(buf), nil
}

// CreateRoom creates a new room with the given title.
func (s *Store) CreateRoom(title string) (*Room, error) {
	slug, err := randomString(slugChars, slugLen)
	if err != nil {
		return nil, err
	}
	token, err := randomString(tokenChars, tokenLen)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(
		`INSERT INTO rooms (slug, admin_token, title) VALUES (?, ?, ?)`,
		slug, token, title,
	)
	if err != nil {
		return nil, fmt.Errorf("insert room: %w", err)
	}
	id, _ := res.LastInsertId()
	return s.getRoom("id = ?", id)
}

// GetRoomBySlug returns the room with the given slug.
func (s *Store) GetRoomBySlug(slug string) (*Room, error) {
	return s.getRoom("slug = ?", slug)
}

// GetRoomByAdminToken returns the room with the given admin token.
func (s *Store) GetRoomByAdminToken(token string) (*Room, error) {
	return s.getRoom("admin_token = ?", token)
}

// ListRooms returns all rooms ordered by creation time descending.
func (s *Store) ListRooms() ([]Room, error) {
	rows, err := s.db.Query(
		`SELECT id, slug, admin_token, title, created_at, archived FROM rooms ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	defer rows.Close()
	var rooms []Room
	for rows.Next() {
		var r Room
		if err := rows.Scan(&r.ID, &r.Slug, &r.AdminToken, &r.Title, &r.CreatedAt, &r.Archived); err != nil {
			return nil, fmt.Errorf("scan room: %w", err)
		}
		rooms = append(rooms, r)
	}
	return rooms, rows.Err()
}

func (s *Store) getRoom(where string, arg any) (*Room, error) {
	var r Room
	err := s.db.QueryRow(
		`SELECT id, slug, admin_token, title, created_at, archived FROM rooms WHERE `+where,
		arg,
	).Scan(&r.ID, &r.Slug, &r.AdminToken, &r.Title, &r.CreatedAt, &r.Archived)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get room: %w", err)
	}
	return &r, nil
}
