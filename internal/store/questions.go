package store

import (
	"fmt"
)

// Question represents a student question.
type Question struct {
	ID        int64    `json:"id"`
	RoomID    int64    `json:"room_id"`
	Body      string   `json:"body"`
	VoterID   string   `json:"-"`
	VoteCount int      `json:"vote_count"`
	Answered  bool     `json:"answered"`
	CreatedAt int64    `json:"created_at"`
	Media     []Media  `json:"media,omitempty"`
	Answers   []Answer `json:"answers"`
}

// CreateQuestion inserts a new question into the given room.
func (s *Store) CreateQuestion(roomID int64, body, voterID string) (*Question, error) {
	res, err := s.db.Exec(
		`INSERT INTO questions (room_id, body, voter_id) VALUES (?, ?, ?)`,
		roomID, body, voterID,
	)
	if err != nil {
		return nil, fmt.Errorf("insert question: %w", err)
	}
	id, _ := res.LastInsertId()
	return s.GetQuestion(id)
}

// ListQuestions returns all questions for a room, sorted by votes or newest.
func (s *Store) ListQuestions(roomID int64, sort string) ([]Question, error) {
	orderBy := "vote_count DESC, created_at DESC"
	if sort == "newest" {
		orderBy = "created_at DESC"
	}
	rows, err := s.db.Query(
		`SELECT id, room_id, body, voter_id, vote_count, answered, created_at
		 FROM questions WHERE room_id = ? ORDER BY `+orderBy,
		roomID,
	)
	if err != nil {
		return nil, fmt.Errorf("list questions: %w", err)
	}
	defer rows.Close()
	var questions []Question
	for rows.Next() {
		var q Question
		if err := rows.Scan(&q.ID, &q.RoomID, &q.Body, &q.VoterID, &q.VoteCount, &q.Answered, &q.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan question: %w", err)
		}
		questions = append(questions, q)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Attach media and answers for each question.
	for i := range questions {
		if err := s.attachQuestionExtras(&questions[i]); err != nil {
			return nil, err
		}
	}
	return questions, nil
}

// ListQuestionsFiltered returns questions optionally filtered by answered state.
func (s *Store) ListQuestionsFiltered(roomID int64, filter string) ([]Question, error) {
	query := `SELECT id, room_id, body, voter_id, vote_count, answered, created_at
	          FROM questions WHERE room_id = ?`
	if filter == "unanswered" {
		query += " AND answered = 0"
	}
	query += " ORDER BY vote_count DESC, created_at DESC"
	rows, err := s.db.Query(query, roomID)
	if err != nil {
		return nil, fmt.Errorf("list questions filtered: %w", err)
	}
	defer rows.Close()
	var questions []Question
	for rows.Next() {
		var q Question
		if err := rows.Scan(&q.ID, &q.RoomID, &q.Body, &q.VoterID, &q.VoteCount, &q.Answered, &q.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan question: %w", err)
		}
		questions = append(questions, q)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range questions {
		if err := s.attachQuestionExtras(&questions[i]); err != nil {
			return nil, err
		}
	}
	return questions, nil
}

// GetQuestion returns a single question by ID with its media and answers.
func (s *Store) GetQuestion(id int64) (*Question, error) {
	var q Question
	err := s.db.QueryRow(
		`SELECT id, room_id, body, voter_id, vote_count, answered, created_at
		 FROM questions WHERE id = ?`, id,
	).Scan(&q.ID, &q.RoomID, &q.Body, &q.VoterID, &q.VoteCount, &q.Answered, &q.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get question: %w", err)
	}
	if err := s.attachQuestionExtras(&q); err != nil {
		return nil, err
	}
	return &q, nil
}

// attachQuestionExtras loads media and all answer versions onto a question.
func (s *Store) attachQuestionExtras(q *Question) error {
	media, err := s.ListMedia("question", q.ID)
	if err != nil {
		return err
	}
	q.Media = media
	answers, err := s.GetAnswers(q.ID)
	if err != nil {
		return err
	}
	if answers == nil {
		answers = []Answer{}
	}
	q.Answers = answers
	return nil
}

// DeleteQuestion removes a question and its related data.
func (s *Store) DeleteQuestion(id int64) error {
	_, err := s.db.Exec(`DELETE FROM questions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete question: %w", err)
	}
	return nil
}
