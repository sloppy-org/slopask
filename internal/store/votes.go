package store

import (
	"fmt"
)

// Vote registers a vote from voterID on the given question.
// It enforces uniqueness and updates the denormalized vote_count.
func (s *Store) Vote(questionID int64, voterID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		`INSERT INTO votes (question_id, voter_id) VALUES (?, ?)`,
		questionID, voterID,
	)
	if err != nil {
		return fmt.Errorf("insert vote: %w", err)
	}
	_, err = tx.Exec(
		`UPDATE questions SET vote_count = vote_count + 1 WHERE id = ?`,
		questionID,
	)
	if err != nil {
		return fmt.Errorf("update vote count: %w", err)
	}
	return tx.Commit()
}

// Unvote removes a vote from voterID on the given question.
func (s *Store) Unvote(questionID int64, voterID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`DELETE FROM votes WHERE question_id = ? AND voter_id = ?`,
		questionID, voterID,
	)
	if err != nil {
		return fmt.Errorf("delete vote: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	_, err = tx.Exec(
		`UPDATE questions SET vote_count = vote_count - 1 WHERE id = ?`,
		questionID,
	)
	if err != nil {
		return fmt.Errorf("update vote count: %w", err)
	}
	return tx.Commit()
}

// HasVoted returns true if voterID has voted on the given question.
func (s *Store) HasVoted(questionID int64, voterID string) (bool, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM votes WHERE question_id = ? AND voter_id = ?`,
		questionID, voterID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("has voted: %w", err)
	}
	return count > 0, nil
}
