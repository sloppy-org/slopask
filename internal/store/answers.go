package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// Answer represents a lecturer's answer version to a question.
type Answer struct {
	ID         int64   `json:"id"`
	QuestionID int64   `json:"question_id"`
	Version    int     `json:"version"`
	Body       string  `json:"body"`
	ThumbsUp   int     `json:"thumbs_up"`
	ThumbsDown int     `json:"thumbs_down"`
	CreatedAt  int64   `json:"created_at"`
	Media      []Media `json:"media,omitempty"`
}

// CreateAnswer inserts a new answer version for the given question and marks it as answered.
// The version auto-increments based on existing answers for that question.
func (s *Store) CreateAnswer(questionID int64, body string) (*Answer, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var maxVersion sql.NullInt64
	err = tx.QueryRow(
		`SELECT MAX(version) FROM answers WHERE question_id = ?`,
		questionID,
	).Scan(&maxVersion)
	if err != nil {
		return nil, fmt.Errorf("get max version: %w", err)
	}

	nextVersion := 1
	if maxVersion.Valid {
		nextVersion = int(maxVersion.Int64) + 1
	}

	res, err := tx.Exec(
		`INSERT INTO answers (question_id, version, body) VALUES (?, ?, ?)`,
		questionID, nextVersion, body,
	)
	if err != nil {
		return nil, fmt.Errorf("insert answer: %w", err)
	}
	_, err = tx.Exec(
		`UPDATE questions SET answered = 1 WHERE id = ?`,
		questionID,
	)
	if err != nil {
		return nil, fmt.Errorf("mark answered: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	id, _ := res.LastInsertId()
	return s.GetAnswerByID(id)
}

// GetAnswers returns all answer versions for the given question, ordered by version ASC.
func (s *Store) GetAnswers(questionID int64) ([]Answer, error) {
	rows, err := s.db.Query(
		`SELECT id, question_id, version, body, thumbs_up, thumbs_down, created_at
		 FROM answers WHERE question_id = ? ORDER BY version ASC`,
		questionID,
	)
	if err != nil {
		return nil, fmt.Errorf("get answers: %w", err)
	}
	defer rows.Close()

	var answers []Answer
	for rows.Next() {
		var a Answer
		if err := rows.Scan(&a.ID, &a.QuestionID, &a.Version, &a.Body, &a.ThumbsUp, &a.ThumbsDown, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan answer: %w", err)
		}
		media, err := s.ListMedia("answer", a.ID)
		if err != nil {
			return nil, err
		}
		a.Media = media
		answers = append(answers, a)
	}
	return answers, rows.Err()
}

// GetLatestAnswer returns the latest version answer for the given question, or nil if none.
func (s *Store) GetLatestAnswer(questionID int64) (*Answer, error) {
	var a Answer
	err := s.db.QueryRow(
		`SELECT id, question_id, version, body, thumbs_up, thumbs_down, created_at
		 FROM answers WHERE question_id = ? ORDER BY version DESC LIMIT 1`,
		questionID,
	).Scan(&a.ID, &a.QuestionID, &a.Version, &a.Body, &a.ThumbsUp, &a.ThumbsDown, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get latest answer: %w", err)
	}
	media, err := s.ListMedia("answer", a.ID)
	if err != nil {
		return nil, err
	}
	a.Media = media
	return &a, nil
}

// GetAnswerByID returns a single answer by its ID.
func (s *Store) GetAnswerByID(id int64) (*Answer, error) {
	var a Answer
	err := s.db.QueryRow(
		`SELECT id, question_id, version, body, thumbs_up, thumbs_down, created_at
		 FROM answers WHERE id = ?`,
		id,
	).Scan(&a.ID, &a.QuestionID, &a.Version, &a.Body, &a.ThumbsUp, &a.ThumbsDown, &a.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get answer by id: %w", err)
	}
	media, err := s.ListMedia("answer", a.ID)
	if err != nil {
		return nil, err
	}
	a.Media = media
	return &a, nil
}

// UpdateAnswerBody updates the text body of an answer version.
func (s *Store) UpdateAnswerBody(answerID int64, body string) (*Answer, error) {
	_, err := s.db.Exec(`UPDATE answers SET body = ? WHERE id = ?`, body, answerID)
	if err != nil {
		return nil, fmt.Errorf("update answer body: %w", err)
	}
	return s.GetAnswerByID(answerID)
}

// VoteAnswer records or updates a thumbs vote on an answer.
// direction must be 1 (up) or -1 (down).
func (s *Store) VoteAnswer(answerID int64, voterID string, direction int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		`INSERT INTO answer_votes (answer_id, voter_id, direction) VALUES (?, ?, ?)
		 ON CONFLICT(answer_id, voter_id) DO UPDATE SET direction = excluded.direction`,
		answerID, voterID, direction,
	)
	if err != nil {
		return fmt.Errorf("upsert answer vote: %w", err)
	}

	if err := recalcAnswerThumbs(tx, answerID); err != nil {
		return err
	}

	return tx.Commit()
}

// UnvoteAnswer removes a thumbs vote on an answer.
func (s *Store) UnvoteAnswer(answerID int64, voterID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`DELETE FROM answer_votes WHERE answer_id = ? AND voter_id = ?`,
		answerID, voterID,
	)
	if err != nil {
		return fmt.Errorf("delete answer vote: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}

	if err := recalcAnswerThumbs(tx, answerID); err != nil {
		return err
	}

	return tx.Commit()
}

// GetAnswerVote returns the vote direction for a voter on an answer (0 if no vote).
func (s *Store) GetAnswerVote(answerID int64, voterID string) (int, error) {
	var direction int
	err := s.db.QueryRow(
		`SELECT direction FROM answer_votes WHERE answer_id = ? AND voter_id = ?`,
		answerID, voterID,
	).Scan(&direction)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get answer vote: %w", err)
	}
	return direction, nil
}

// recalcAnswerThumbs recalculates denormalized thumbs_up and thumbs_down on an answer.
func recalcAnswerThumbs(tx *sql.Tx, answerID int64) error {
	var up, down int
	err := tx.QueryRow(
		`SELECT COALESCE(SUM(CASE WHEN direction = 1 THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN direction = -1 THEN 1 ELSE 0 END), 0)
		 FROM answer_votes WHERE answer_id = ?`,
		answerID,
	).Scan(&up, &down)
	if err != nil {
		return fmt.Errorf("recalc thumbs: %w", err)
	}

	_, err = tx.Exec(
		`UPDATE answers SET thumbs_up = ?, thumbs_down = ? WHERE id = ?`,
		up, down, answerID,
	)
	if err != nil {
		return fmt.Errorf("update thumbs: %w", err)
	}
	return nil
}
