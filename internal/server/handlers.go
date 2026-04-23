package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/sloppy-org/slopask/internal/store"
)

// Upload size limits by media category.
const (
	maxImageSize = 10 << 20  // 10 MB
	maxAudioSize = 50 << 20  // 50 MB
	maxVideoSize = 200 << 20 // 200 MB
)

// Allowed MIME types by category.
var allowedMIME = map[string]map[string]bool{
	"image": {
		"image/jpeg": true,
		"image/png":  true,
		"image/gif":  true,
		"image/webp": true,
	},
	"audio": {
		"audio/webm": true,
		"audio/ogg":  true,
		"audio/mp4":  true,
		"audio/mpeg": true,
		"audio/wav":  true,
		"audio/wave": true,
	},
	"video": {
		"video/webm": true,
		"video/mp4":  true,
		"video/ogg":  true,
	},
}

var roomTmpl = template.Must(template.ParseFS(staticFS, "static/room.html"))
var adminTmpl = template.Must(template.ParseFS(staticFS, "static/admin.html"))
var impressumTmpl = template.Must(template.ParseFS(staticFS, "static/legalnotes.html"))

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleImpressum(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	impressumTmpl.Execute(w, nil)
}

func (s *Server) handleRoom(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	room, err := s.store.GetRoomBySlug(slug)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	roomTmpl.Execute(w, room)
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	room, err := s.store.GetRoomByAdminToken(token)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	adminTmpl.Execute(w, room)
}

func (s *Server) handleListQuestions(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	room, err := s.store.GetRoomBySlug(slug)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	sort := r.URL.Query().Get("sort")
	questions, err := s.store.ListQuestions(room.ID, sort)
	if err != nil {
		serverError(w, err)
		return
	}
	if questions == nil {
		questions = []store.Question{}
	}
	writeJSON(w, http.StatusOK, questions)
}

func (s *Server) handleCreateQuestion(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	room, err := s.store.GetRoomBySlug(slug)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	if err := r.ParseMultipartForm(maxVideoSize + (1 << 20)); err != nil {
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}

	body := r.FormValue("body")
	voterID := r.FormValue("voter_id")
	mediaFiles := r.MultipartForm.File["media"]
	if body == "" && len(mediaFiles) == 0 {
		http.Error(w, "body or media is required", http.StatusBadRequest)
		return
	}

	q, err := s.store.CreateQuestion(room.ID, body, voterID)
	if err != nil {
		serverError(w, err)
		return
	}

	for _, fh := range mediaFiles {
		m, saveErr := s.saveUpload(fh, room.ID, "question", q.ID)
		if saveErr != nil {
			log.Printf("upload error: %v", saveErr)
			continue
		}
		q.Media = append(q.Media, *m)
	}

	data, _ := json.Marshal(q)
	s.broker.publish(room.ID, "question_new", string(data))
	writeJSON(w, http.StatusCreated, q)
}

func (s *Server) handleVote(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	room, err := s.store.GetRoomBySlug(slug)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		VoterID string `json:"voter_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	qid, err := parseQID(r)
	if err != nil {
		http.Error(w, "invalid question id", http.StatusBadRequest)
		return
	}

	if err := s.store.Vote(qid, req.VoterID); err != nil {
		http.Error(w, "already voted", http.StatusConflict)
		return
	}

	q, err := s.store.GetQuestion(qid)
	if err != nil {
		serverError(w, err)
		return
	}

	data, _ := json.Marshal(map[string]int64{"question_id": qid, "vote_count": int64(q.VoteCount)})
	s.broker.publish(room.ID, "question_vote", string(data))
	writeJSON(w, http.StatusOK, map[string]int64{"question_id": qid, "vote_count": int64(q.VoteCount)})
}

func (s *Server) handleUnvote(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	room, err := s.store.GetRoomBySlug(slug)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		VoterID string `json:"voter_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	qid, err := parseQID(r)
	if err != nil {
		http.Error(w, "invalid question id", http.StatusBadRequest)
		return
	}

	if err := s.store.Unvote(qid, req.VoterID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "vote not found", http.StatusNotFound)
			return
		}
		serverError(w, err)
		return
	}

	q, err := s.store.GetQuestion(qid)
	if err != nil {
		serverError(w, err)
		return
	}

	data, _ := json.Marshal(map[string]int64{"question_id": qid, "vote_count": int64(q.VoteCount)})
	s.broker.publish(room.ID, "question_vote", string(data))
	writeJSON(w, http.StatusOK, map[string]int64{"question_id": qid, "vote_count": int64(q.VoteCount)})
}

func (s *Server) handleStudentSSE(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	room, err := s.store.GetRoomBySlug(slug)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	s.broker.serveSSE(w, r, room.ID)
}

// Admin handlers.

func (s *Server) handleAdminCreateQuestion(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	room, err := s.store.GetRoomByAdminToken(token)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	if err := r.ParseMultipartForm(maxVideoSize + (1 << 20)); err != nil {
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}

	body := r.FormValue("body")
	voterID := r.FormValue("voter_id")
	mediaFiles := r.MultipartForm.File["media"]
	if body == "" && len(mediaFiles) == 0 {
		http.Error(w, "body or media is required", http.StatusBadRequest)
		return
	}

	q, err := s.store.CreateQuestion(room.ID, body, voterID)
	if err != nil {
		serverError(w, err)
		return
	}

	for _, fh := range mediaFiles {
		m, saveErr := s.saveUpload(fh, room.ID, "question", q.ID)
		if saveErr != nil {
			log.Printf("upload error: %v", saveErr)
			continue
		}
		q.Media = append(q.Media, *m)
	}

	data, _ := json.Marshal(q)
	s.broker.publish(room.ID, "question_new", string(data))
	writeJSON(w, http.StatusCreated, q)
}

func (s *Server) handleAdminListQuestions(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	room, err := s.store.GetRoomByAdminToken(token)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	sort := r.URL.Query().Get("sort")
	questions, err := s.store.ListQuestions(room.ID, sort)
	if err != nil {
		serverError(w, err)
		return
	}
	if questions == nil {
		questions = []store.Question{}
	}
	writeJSON(w, http.StatusOK, questions)
}

func (s *Server) handleUpdateQuestion(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	room, err := s.store.GetRoomByAdminToken(token)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	qid, err := parseQID(r)
	if err != nil {
		http.Error(w, "invalid question id", http.StatusBadRequest)
		return
	}

	existing, err := s.store.GetQuestion(qid)
	if err != nil {
		serverError(w, err)
		return
	}
	if existing.RoomID != room.ID {
		http.NotFound(w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	q, err := s.store.UpdateQuestionBody(qid, req.Body)
	if err != nil {
		serverError(w, err)
		return
	}

	data, _ := json.Marshal(q)
	s.broker.publish(room.ID, "question_update", string(data))
	writeJSON(w, http.StatusOK, q)
}

func (s *Server) handleCreateAnswer(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	room, err := s.store.GetRoomByAdminToken(token)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	if err := r.ParseMultipartForm(maxVideoSize + (1 << 20)); err != nil {
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}

	qid, err := parseQID(r)
	if err != nil {
		http.Error(w, "invalid question id", http.StatusBadRequest)
		return
	}

	existing, err := s.store.GetQuestion(qid)
	if err != nil {
		serverError(w, err)
		return
	}
	if existing.RoomID != room.ID {
		http.NotFound(w, r)
		return
	}

	body := r.FormValue("body")
	answer, err := s.store.CreateAnswer(qid, body)
	if err != nil {
		serverError(w, err)
		return
	}

	mediaFiles := r.MultipartForm.File["media"]
	for _, fh := range mediaFiles {
		m, saveErr := s.saveUpload(fh, room.ID, "answer", answer.ID)
		if saveErr != nil {
			log.Printf("upload error: %v", saveErr)
			continue
		}
		answer.Media = append(answer.Media, *m)
	}

	allAnswers, err := s.store.GetAnswers(qid)
	if err != nil {
		serverError(w, err)
		return
	}
	data, _ := json.Marshal(map[string]any{"question_id": qid, "answers": allAnswers})
	s.broker.publish(room.ID, "answer_new", string(data))
	writeJSON(w, http.StatusCreated, answer)
}

// deleteQuestionFull collects media files, deletes the DB rows, then removes files from disk.
func (s *Server) deleteQuestionFull(qid int64, roomID int64) error {
	paths, _ := s.store.CollectMediaPaths(qid)
	if err := s.store.DeleteQuestion(qid); err != nil {
		return err
	}
	for _, p := range paths {
		os.Remove(filepath.Join(s.uploadsDir, p))
	}
	data, _ := json.Marshal(map[string]int64{"question_id": qid})
	s.broker.publish(roomID, "question_delete", string(data))
	return nil
}

func (s *Server) handleUserEditQuestion(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	_, err := s.store.GetRoomBySlug(slug)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	qid, err := parseQID(r)
	if err != nil {
		http.Error(w, "invalid question id", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Body    string `json:"body"`
		VoterID string `json:"voter_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	ownerID, err := s.store.GetQuestionVoterID(qid)
	if err != nil {
		serverError(w, err)
		return
	}
	if ownerID != req.VoterID {
		http.Error(w, "not your question", http.StatusForbidden)
		return
	}

	q, err := s.store.UpdateQuestionBody(qid, req.Body)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, q)
}

func (s *Server) handleUserDeleteQuestion(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	room, err := s.store.GetRoomBySlug(slug)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	qid, err := parseQID(r)
	if err != nil {
		http.Error(w, "invalid question id", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		VoterID string `json:"voter_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VoterID == "" {
		http.Error(w, "voter_id required", http.StatusBadRequest)
		return
	}

	ownerID, err := s.store.GetQuestionVoterID(qid)
	if err != nil {
		serverError(w, err)
		return
	}
	if ownerID != req.VoterID {
		http.Error(w, "not your question", http.StatusForbidden)
		return
	}

	if err := s.deleteQuestionFull(qid, room.ID); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUserDeleteMedia(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	_, err := s.store.GetRoomBySlug(slug)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	qid, err := parseQID(r)
	if err != nil {
		http.Error(w, "invalid question id", http.StatusBadRequest)
		return
	}

	// Check ownership via X-Voter-ID header.
	voterID := r.Header.Get("X-Voter-ID")
	if voterID == "" {
		http.Error(w, "voter_id required", http.StatusBadRequest)
		return
	}
	ownerID, err := s.store.GetQuestionVoterID(qid)
	if err != nil {
		serverError(w, err)
		return
	}
	if ownerID != voterID {
		http.Error(w, "not your question", http.StatusForbidden)
		return
	}

	midStr := chi.URLParam(r, "mid")
	mid, err := strconv.ParseInt(midStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid media id", http.StatusBadRequest)
		return
	}

	diskPath, err := s.store.DeleteMedia("question", mid)
	if err != nil {
		serverError(w, err)
		return
	}
	fullPath := filepath.Join(s.uploadsDir, diskPath)
	os.Remove(fullPath)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteMedia(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	room, err := s.store.GetRoomByAdminToken(token)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	midStr := chi.URLParam(r, "mid")
	mid, err := strconv.ParseInt(midStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid media id", http.StatusBadRequest)
		return
	}

	// Determine parent type from URL path.
	parentType := "question"
	if strings.Contains(r.URL.Path, "/media/answer/") {
		parentType = "answer"
	}

	mediaRoomID, err := s.store.GetMediaRoomID(parentType, mid)
	if err != nil {
		serverError(w, err)
		return
	}
	if mediaRoomID != room.ID {
		http.NotFound(w, r)
		return
	}

	diskPath, err := s.store.DeleteMedia(parentType, mid)
	if err != nil {
		serverError(w, err)
		return
	}

	// Remove file from disk.
	fullPath := filepath.Join(s.uploadsDir, diskPath)
	os.Remove(fullPath)

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdateAnswer(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	room, err := s.store.GetRoomByAdminToken(token)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	aidStr := chi.URLParam(r, "aid")
	aid, err := strconv.ParseInt(aidStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid answer id", http.StatusBadRequest)
		return
	}

	existing, err := s.store.GetAnswerByID(aid)
	if err != nil {
		serverError(w, err)
		return
	}
	q, err := s.store.GetQuestion(existing.QuestionID)
	if err != nil {
		serverError(w, err)
		return
	}
	if q.RoomID != room.ID {
		http.NotFound(w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	a, err := s.store.UpdateAnswerBody(aid, req.Body)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleDeleteQuestion(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	room, err := s.store.GetRoomByAdminToken(token)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	qid, err := parseQID(r)
	if err != nil {
		http.Error(w, "invalid question id", http.StatusBadRequest)
		return
	}

	q, err := s.store.GetQuestion(qid)
	if err != nil {
		serverError(w, err)
		return
	}
	if q.RoomID != room.ID {
		http.NotFound(w, r)
		return
	}

	if err := s.deleteQuestionFull(qid, room.ID); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAdminSSE(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	room, err := s.store.GetRoomByAdminToken(token)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	s.broker.serveSSE(w, r, room.ID)
}

// External API handlers.

func (s *Server) handleAPIListQuestions(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	room, err := s.store.GetRoomByAdminToken(token)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	filter := r.URL.Query().Get("filter")
	sort := r.URL.Query().Get("sort")
	questions, err := s.store.ListQuestionsFiltered(room.ID, filter, sort)
	if err != nil {
		serverError(w, err)
		return
	}
	if questions == nil {
		questions = []store.Question{}
	}
	writeJSON(w, http.StatusOK, questions)
}

func (s *Server) handleAPIGetQuestion(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	room, err := s.store.GetRoomByAdminToken(token)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	qid, err := parseQID(r)
	if err != nil {
		http.Error(w, "invalid question id", http.StatusBadRequest)
		return
	}

	q, err := s.store.GetQuestion(qid)
	if err != nil {
		serverError(w, err)
		return
	}
	if q.RoomID != room.ID {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, q)
}

func (s *Server) handleAPICreateAnswer(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	room, err := s.store.GetRoomByAdminToken(token)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	if err := r.ParseMultipartForm(maxVideoSize + (1 << 20)); err != nil {
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}

	qid, err := parseQID(r)
	if err != nil {
		http.Error(w, "invalid question id", http.StatusBadRequest)
		return
	}

	existing, err := s.store.GetQuestion(qid)
	if err != nil {
		serverError(w, err)
		return
	}
	if existing.RoomID != room.ID {
		http.NotFound(w, r)
		return
	}

	body := r.FormValue("body")
	answer, err := s.store.CreateAnswer(qid, body)
	if err != nil {
		serverError(w, err)
		return
	}

	mediaFiles := r.MultipartForm.File["media"]
	for _, fh := range mediaFiles {
		m, saveErr := s.saveUpload(fh, room.ID, "answer", answer.ID)
		if saveErr != nil {
			log.Printf("upload error: %v", saveErr)
			continue
		}
		answer.Media = append(answer.Media, *m)
	}

	allAnswers, err := s.store.GetAnswers(qid)
	if err != nil {
		serverError(w, err)
		return
	}
	data, _ := json.Marshal(map[string]any{"question_id": qid, "answers": allAnswers})
	s.broker.publish(room.ID, "answer_new", string(data))
	writeJSON(w, http.StatusCreated, answer)
}

func (s *Server) handleVoteAnswer(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	room, err := s.store.GetRoomBySlug(slug)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	aid, err := strconv.ParseInt(chi.URLParam(r, "aid"), 10, 64)
	if err != nil {
		http.Error(w, "invalid answer id", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		VoterID   string `json:"voter_id"`
		Direction int    `json:"direction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if req.Direction != -1 && req.Direction != 0 && req.Direction != 1 {
		http.Error(w, "direction must be -1, 0, or 1", http.StatusBadRequest)
		return
	}

	if req.Direction == 0 {
		if err := s.store.UnvoteAnswer(aid, req.VoterID); err != nil && !errors.Is(err, store.ErrNotFound) {
			serverError(w, err)
			return
		}
	} else {
		if err := s.store.VoteAnswer(aid, req.VoterID, req.Direction); err != nil {
			serverError(w, err)
			return
		}
	}

	answer, err := s.store.GetAnswerByID(aid)
	if err != nil {
		serverError(w, err)
		return
	}

	data, _ := json.Marshal(map[string]any{
		"answer_id":  aid,
		"thumbs_up":  answer.ThumbsUp,
		"thumbs_down": answer.ThumbsDown,
	})
	s.broker.publish(room.ID, "answer_vote", string(data))
	writeJSON(w, http.StatusOK, map[string]any{
		"answer_id":  aid,
		"thumbs_up":  answer.ThumbsUp,
		"thumbs_down": answer.ThumbsDown,
	})
}

// Media file serving.

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	// Prevent path traversal.
	clean := filepath.Clean(path)
	if strings.Contains(clean, "..") {
		http.NotFound(w, r)
		return
	}
	fullPath := filepath.Join(s.uploadsDir, clean)

	// Verify the resolved path stays under uploadsDir.
	if !strings.HasPrefix(fullPath, filepath.Clean(s.uploadsDir)+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}

	// Prevent directory listing: only serve regular files.
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	// Ensure correct Content-Type for media formats Go doesn't detect well.
	ext := strings.ToLower(filepath.Ext(clean))
	switch ext {
	case ".webm":
		w.Header().Set("Content-Type", "video/webm")
	case ".ogg":
		w.Header().Set("Content-Type", "audio/ogg")
	}
	http.ServeFile(w, r, fullPath)
}

// Upload handling.

func (s *Server) saveUpload(fh *multipart.FileHeader, roomID int64, parentType string, parentID int64) (*store.Media, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, fmt.Errorf("open upload: %w", err)
	}
	defer f.Close()

	// Read first 512 bytes to detect content type.
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("read header: %w", err)
	}
	detected := http.DetectContentType(buf[:n])

	// Determine media category and validate.
	category := classifyMIME(detected)
	if category == "" {
		return nil, fmt.Errorf("unsupported mime type: %s", detected)
	}

	maxSize := maxSizeForCategory(category)
	if fh.Size > maxSize {
		return nil, fmt.Errorf("file too large: %d > %d", fh.Size, maxSize)
	}

	// Seek back to start.
	if seeker, ok := f.(io.Seeker); ok {
		seeker.Seek(0, io.SeekStart)
	}

	// Generate UUID filename.
	ext := extensionForMIME(detected)
	uuid := generateUUID()
	filename := uuid + ext

	// Build directory path.
	subDir := "q"
	if parentType == "answer" {
		subDir = "a"
	}
	relDir := filepath.Join(fmt.Sprintf("%d", roomID), subDir, fmt.Sprintf("%d", parentID))
	absDir := filepath.Join(s.uploadsDir, relDir)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}

	// Atomic write: temp file + rename.
	tmpFile, err := os.CreateTemp(absDir, ".upload-*")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	written, err := io.Copy(tmpFile, f)
	tmpFile.Close()
	if err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("write upload: %w", err)
	}

	finalPath := filepath.Join(absDir, filename)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("rename upload: %w", err)
	}

	diskPath := filepath.Join(relDir, filename)
	m, err := s.store.CreateMedia(parentType, parentID, category, fh.Filename, diskPath, detected, written)
	if err != nil {
		os.Remove(finalPath)
		return nil, err
	}
	return m, nil
}

func classifyMIME(mimeType string) string {
	for category, types := range allowedMIME {
		if types[mimeType] {
			return category
		}
	}
	return ""
}

func maxSizeForCategory(category string) int64 {
	switch category {
	case "image":
		return maxImageSize
	case "audio":
		return maxAudioSize
	case "video":
		return maxVideoSize
	default:
		return 0
	}
}

func extensionForMIME(mimeType string) string {
	// Prefer common extensions over mime.ExtensionsByType quirks.
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav", "audio/wave":
		return ".wav"
	case "audio/ogg":
		return ".ogg"
	case "audio/webm":
		return ".webm"
	case "audio/mp4":
		return ".m4a"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/ogg":
		return ".ogv"
	default:
		exts, _ := mime.ExtensionsByType(mimeType)
		if len(exts) > 0 {
			return exts[0]
		}
		return ".bin"
	}
}

func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func parseQID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "qid"), 10, 64)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("server error: %v", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
