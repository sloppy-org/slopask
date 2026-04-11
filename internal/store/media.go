package store

import (
	"fmt"
)

// Media represents an uploaded file attached to a question or answer.
type Media struct {
	ID        int64  `json:"id"`
	ParentID  int64  `json:"-"`
	Kind      string `json:"kind"`
	Filename  string `json:"filename"`
	DiskPath  string `json:"-"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt int64  `json:"created_at"`
	URL       string `json:"url"`
}

// CreateMedia records an uploaded file. parentType is "question" or "answer".
func (s *Store) CreateMedia(parentType string, parentID int64, kind, filename, diskPath, mimeType string, sizeBytes int64) (*Media, error) {
	table, col := mediaTableCol(parentType)
	res, err := s.db.Exec(
		fmt.Sprintf(`INSERT INTO %s (%s, kind, filename, disk_path, mime_type, size_bytes) VALUES (?, ?, ?, ?, ?, ?)`, table, col),
		parentID, kind, filename, diskPath, mimeType, sizeBytes,
	)
	if err != nil {
		return nil, fmt.Errorf("insert media: %w", err)
	}
	id, _ := res.LastInsertId()
	return s.getMediaByID(table, col, id)
}

// ListMedia returns all media for a question or answer.
func (s *Store) ListMedia(parentType string, parentID int64) ([]Media, error) {
	table, col := mediaTableCol(parentType)
	rows, err := s.db.Query(
		fmt.Sprintf(`SELECT id, %s, kind, filename, disk_path, mime_type, size_bytes, created_at FROM %s WHERE %s = ? ORDER BY id`, col, table, col),
		parentID,
	)
	if err != nil {
		return nil, fmt.Errorf("list media: %w", err)
	}
	defer rows.Close()
	var media []Media
	for rows.Next() {
		var m Media
		if err := rows.Scan(&m.ID, &m.ParentID, &m.Kind, &m.Filename, &m.DiskPath, &m.MimeType, &m.SizeBytes, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan media: %w", err)
		}
		m.URL = "/media/" + m.DiskPath
		media = append(media, m)
	}
	return media, rows.Err()
}

// GetMedia returns a single media record by its disk_path.
func (s *Store) GetMedia(diskPath string) (*Media, error) {
	var m Media
	err := s.db.QueryRow(
		`SELECT id, kind, filename, disk_path, mime_type, size_bytes, created_at FROM question_media WHERE disk_path = ?
		 UNION ALL
		 SELECT id, kind, filename, disk_path, mime_type, size_bytes, created_at FROM answer_media WHERE disk_path = ?
		 LIMIT 1`,
		diskPath, diskPath,
	).Scan(&m.ID, &m.Kind, &m.Filename, &m.DiskPath, &m.MimeType, &m.SizeBytes, &m.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get media: %w", err)
	}
	m.URL = "/media/" + m.DiskPath
	return &m, nil
}

func (s *Store) getMediaByID(table, col string, id int64) (*Media, error) {
	var m Media
	err := s.db.QueryRow(
		fmt.Sprintf(`SELECT id, %s, kind, filename, disk_path, mime_type, size_bytes, created_at FROM %s WHERE id = ?`, col, table),
		id,
	).Scan(&m.ID, &m.ParentID, &m.Kind, &m.Filename, &m.DiskPath, &m.MimeType, &m.SizeBytes, &m.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get media by id: %w", err)
	}
	m.URL = "/media/" + m.DiskPath
	return &m, nil
}

// DeleteMedia removes a media record and returns its disk_path for file cleanup.
func (s *Store) DeleteMedia(parentType string, mediaID int64) (string, error) {
	table, _ := mediaTableCol(parentType)
	var diskPath string
	err := s.db.QueryRow(
		fmt.Sprintf(`SELECT disk_path FROM %s WHERE id = ?`, table), mediaID,
	).Scan(&diskPath)
	if err != nil {
		return "", fmt.Errorf("get media disk_path: %w", err)
	}
	_, err = s.db.Exec(fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, table), mediaID)
	if err != nil {
		return "", fmt.Errorf("delete media: %w", err)
	}
	return diskPath, nil
}

func mediaTableCol(parentType string) (string, string) {
	if parentType == "answer" {
		return "answer_media", "answer_id"
	}
	return "question_media", "question_id"
}
