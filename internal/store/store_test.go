package store

import (
	"testing"
)

// openMemory creates an in-memory SQLite store for testing.
func openMemory(t *testing.T) *Store {
	t.Helper()
	s, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// --- Room tests ---

func TestCreateRoom(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, err := s.CreateRoom("Test Room")
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if room.Title != "Test Room" {
		t.Errorf("title = %q, want %q", room.Title, "Test Room")
	}
	if room.Slug == "" {
		t.Error("slug is empty")
	}
	if len(room.Slug) != slugLen {
		t.Errorf("slug length = %d, want %d", len(room.Slug), slugLen)
	}
	if room.AdminToken == "" {
		t.Error("admin_token is empty")
	}
	if len(room.AdminToken) != tokenLen {
		t.Errorf("admin_token length = %d, want %d", len(room.AdminToken), tokenLen)
	}
	if room.ID == 0 {
		t.Error("room ID is 0")
	}
	if room.CreatedAt == 0 {
		t.Error("created_at is 0")
	}
	if room.Archived {
		t.Error("new room should not be archived")
	}
}

func TestGetRoomBySlug(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, err := s.CreateRoom("Slug Room")
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	got, err := s.GetRoomBySlug(room.Slug)
	if err != nil {
		t.Fatalf("GetRoomBySlug: %v", err)
	}
	if got.ID != room.ID {
		t.Errorf("got ID %d, want %d", got.ID, room.ID)
	}
	if got.Title != "Slug Room" {
		t.Errorf("title = %q, want %q", got.Title, "Slug Room")
	}
}

func TestGetRoomBySlug_NotFound(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	_, err := s.GetRoomBySlug("nonexistent")
	if err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetRoomByAdminToken(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, err := s.CreateRoom("Token Room")
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	got, err := s.GetRoomByAdminToken(room.AdminToken)
	if err != nil {
		t.Fatalf("GetRoomByAdminToken: %v", err)
	}
	if got.ID != room.ID {
		t.Errorf("got ID %d, want %d", got.ID, room.ID)
	}
}

func TestGetRoomByAdminToken_NotFound(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	_, err := s.GetRoomByAdminToken("badtoken")
	if err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestListRooms(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	r1, err := s.CreateRoom("Room A")
	if err != nil {
		t.Fatalf("CreateRoom A: %v", err)
	}
	r2, err := s.CreateRoom("Room B")
	if err != nil {
		t.Fatalf("CreateRoom B: %v", err)
	}
	rooms, err := s.ListRooms()
	if err != nil {
		t.Fatalf("ListRooms: %v", err)
	}
	if len(rooms) != 2 {
		t.Fatalf("len = %d, want 2", len(rooms))
	}
	// Both rooms created in the same second, so ordering is by created_at DESC
	// with ties broken arbitrarily. Just verify both are present.
	titles := map[string]bool{rooms[0].Title: true, rooms[1].Title: true}
	if !titles["Room A"] || !titles["Room B"] {
		t.Errorf("expected both Room A and Room B, got %q and %q", rooms[0].Title, rooms[1].Title)
	}
	_ = r1
	_ = r2
}

func TestListRooms_Empty(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	rooms, err := s.ListRooms()
	if err != nil {
		t.Fatalf("ListRooms: %v", err)
	}
	if rooms != nil {
		t.Errorf("expected nil, got %d rooms", len(rooms))
	}
}

func TestUniqueSlugAndToken(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	// Create many rooms and verify all slugs and tokens are distinct.
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		room, err := s.CreateRoom("Room")
		if err != nil {
			t.Fatalf("CreateRoom %d: %v", i, err)
		}
		if seen[room.Slug] {
			t.Errorf("duplicate slug: %s", room.Slug)
		}
		if seen[room.AdminToken] {
			t.Errorf("duplicate token: %s", room.AdminToken)
		}
		seen[room.Slug] = true
		seen[room.AdminToken] = true
	}
}

// --- Question tests ---

func TestCreateQuestion(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, err := s.CreateRoom("Q Room")
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	q, err := s.CreateQuestion(room.ID, "How does X work?", "voter-1")
	if err != nil {
		t.Fatalf("CreateQuestion: %v", err)
	}
	if q.Body != "How does X work?" {
		t.Errorf("body = %q", q.Body)
	}
	if q.RoomID != room.ID {
		t.Errorf("room_id = %d, want %d", q.RoomID, room.ID)
	}
	if q.VoteCount != 0 {
		t.Errorf("vote_count = %d, want 0", q.VoteCount)
	}
	if q.Answered {
		t.Error("new question should not be answered")
	}
	if q.Answers == nil {
		t.Error("answers should be non-nil empty slice")
	}
	if len(q.Answers) != 0 {
		t.Errorf("answers len = %d, want 0", len(q.Answers))
	}
}

func TestListQuestions_ByVotes(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Vote Sort Room")
	q1, _ := s.CreateQuestion(room.ID, "Q1", "v1")
	q2, _ := s.CreateQuestion(room.ID, "Q2", "v2")

	// Vote on q1 twice, q2 once.
	s.Vote(q1.ID, "voter-a")
	s.Vote(q1.ID, "voter-b")
	s.Vote(q2.ID, "voter-c")

	questions, err := s.ListQuestions(room.ID, "votes")
	if err != nil {
		t.Fatalf("ListQuestions: %v", err)
	}
	if len(questions) != 2 {
		t.Fatalf("len = %d, want 2", len(questions))
	}
	if questions[0].ID != q1.ID {
		t.Errorf("first question ID = %d, want %d (most votes)", questions[0].ID, q1.ID)
	}
	if questions[0].VoteCount != 2 {
		t.Errorf("first vote_count = %d, want 2", questions[0].VoteCount)
	}
}

func TestListQuestions_ByNewest(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Newest Sort Room")
	s.CreateQuestion(room.ID, "Q1", "v1")
	q2, _ := s.CreateQuestion(room.ID, "Q2", "v2")

	questions, err := s.ListQuestions(room.ID, "newest")
	if err != nil {
		t.Fatalf("ListQuestions: %v", err)
	}
	if len(questions) != 2 {
		t.Fatalf("len = %d, want 2", len(questions))
	}
	// newest first
	if questions[0].ID != q2.ID {
		t.Errorf("first question ID = %d, want %d (newest)", questions[0].ID, q2.ID)
	}
}

func TestGetQuestion(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Get Q Room")
	q, _ := s.CreateQuestion(room.ID, "What is Y?", "v1")

	got, err := s.GetQuestion(q.ID)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if got.Body != "What is Y?" {
		t.Errorf("body = %q", got.Body)
	}
	if got.Answers == nil {
		t.Error("answers should be non-nil empty slice")
	}
}

func TestDeleteQuestion_CascadeVotesAnswersMedia(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Delete Q Room")
	q, _ := s.CreateQuestion(room.ID, "Delete me", "v1")

	// Add vote, answer, and media.
	s.Vote(q.ID, "voter-x")
	a, _ := s.CreateAnswer(q.ID, "The answer")
	s.CreateMedia("question", q.ID, "image", "photo.jpg", "1/q/1/abc.jpg", "image/jpeg", 100)
	s.CreateMedia("answer", a.ID, "image", "ans.jpg", "1/a/1/def.jpg", "image/jpeg", 200)

	err := s.DeleteQuestion(q.ID)
	if err != nil {
		t.Fatalf("DeleteQuestion: %v", err)
	}

	// Verify cascade: votes, answers, and media should be gone.
	voted, _ := s.HasVoted(q.ID, "voter-x")
	if voted {
		t.Error("vote should be cascaded on delete")
	}
	answers, _ := s.GetAnswers(q.ID)
	if len(answers) != 0 {
		t.Errorf("answers should be empty after cascade, got %d", len(answers))
	}
	qMedia, _ := s.ListMedia("question", q.ID)
	if len(qMedia) != 0 {
		t.Errorf("question media should be empty after cascade, got %d", len(qMedia))
	}
}

// --- Vote tests ---

func TestVote_And_HasVoted(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Vote Room")
	q, _ := s.CreateQuestion(room.ID, "Voteable", "v1")

	err := s.Vote(q.ID, "voter-1")
	if err != nil {
		t.Fatalf("Vote: %v", err)
	}

	voted, err := s.HasVoted(q.ID, "voter-1")
	if err != nil {
		t.Fatalf("HasVoted: %v", err)
	}
	if !voted {
		t.Error("expected HasVoted = true")
	}

	// Check denormalized count.
	got, _ := s.GetQuestion(q.ID)
	if got.VoteCount != 1 {
		t.Errorf("vote_count = %d, want 1", got.VoteCount)
	}
}

func TestUnvote(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Unvote Room")
	q, _ := s.CreateQuestion(room.ID, "Unvoteable", "v1")

	s.Vote(q.ID, "voter-1")

	err := s.Unvote(q.ID, "voter-1")
	if err != nil {
		t.Fatalf("Unvote: %v", err)
	}

	voted, _ := s.HasVoted(q.ID, "voter-1")
	if voted {
		t.Error("expected HasVoted = false after unvote")
	}

	got, _ := s.GetQuestion(q.ID)
	if got.VoteCount != 0 {
		t.Errorf("vote_count = %d, want 0", got.VoteCount)
	}
}

func TestVote_Duplicate(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Dup Vote Room")
	q, _ := s.CreateQuestion(room.ID, "Dup", "v1")

	s.Vote(q.ID, "voter-1")
	err := s.Vote(q.ID, "voter-1")
	if err == nil {
		t.Error("expected error on duplicate vote")
	}

	// Count should still be 1.
	got, _ := s.GetQuestion(q.ID)
	if got.VoteCount != 1 {
		t.Errorf("vote_count = %d, want 1 after duplicate attempt", got.VoteCount)
	}
}

func TestUnvote_NotFound(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Unvote NF Room")
	q, _ := s.CreateQuestion(room.ID, "NF", "v1")

	err := s.Unvote(q.ID, "nobody")
	if err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestHasVoted_NoVote(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("HasVoted Room")
	q, _ := s.CreateQuestion(room.ID, "Q", "v1")

	voted, err := s.HasVoted(q.ID, "nobody")
	if err != nil {
		t.Fatalf("HasVoted: %v", err)
	}
	if voted {
		t.Error("expected HasVoted = false for non-voter")
	}
}

// --- Answer tests ---

func TestCreateAnswer_VersionAutoIncrement(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Answer Room")
	q, _ := s.CreateQuestion(room.ID, "Answerable", "v1")

	a1, err := s.CreateAnswer(q.ID, "First answer")
	if err != nil {
		t.Fatalf("CreateAnswer v1: %v", err)
	}
	if a1.Version != 1 {
		t.Errorf("version = %d, want 1", a1.Version)
	}
	if a1.Body != "First answer" {
		t.Errorf("body = %q", a1.Body)
	}

	a2, err := s.CreateAnswer(q.ID, "Second answer")
	if err != nil {
		t.Fatalf("CreateAnswer v2: %v", err)
	}
	if a2.Version != 2 {
		t.Errorf("version = %d, want 2", a2.Version)
	}

	a3, err := s.CreateAnswer(q.ID, "Third answer")
	if err != nil {
		t.Fatalf("CreateAnswer v3: %v", err)
	}
	if a3.Version != 3 {
		t.Errorf("version = %d, want 3", a3.Version)
	}

	// Creating an answer marks the question as answered.
	got, _ := s.GetQuestion(q.ID)
	if !got.Answered {
		t.Error("question should be marked as answered")
	}
}

func TestGetAnswers_OrderedASC(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Answers Room")
	q, _ := s.CreateQuestion(room.ID, "Multi-answer", "v1")

	s.CreateAnswer(q.ID, "v1")
	s.CreateAnswer(q.ID, "v2")
	s.CreateAnswer(q.ID, "v3")

	answers, err := s.GetAnswers(q.ID)
	if err != nil {
		t.Fatalf("GetAnswers: %v", err)
	}
	if len(answers) != 3 {
		t.Fatalf("len = %d, want 3", len(answers))
	}
	for i, a := range answers {
		if a.Version != i+1 {
			t.Errorf("answers[%d].Version = %d, want %d", i, a.Version, i+1)
		}
	}
}

func TestGetLatestAnswer(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Latest Answer Room")
	q, _ := s.CreateQuestion(room.ID, "Latest", "v1")

	// No answers yet.
	latest, err := s.GetLatestAnswer(q.ID)
	if err != nil {
		t.Fatalf("GetLatestAnswer (none): %v", err)
	}
	if latest != nil {
		t.Error("expected nil when no answers exist")
	}

	s.CreateAnswer(q.ID, "v1")
	s.CreateAnswer(q.ID, "v2")

	latest, err = s.GetLatestAnswer(q.ID)
	if err != nil {
		t.Fatalf("GetLatestAnswer: %v", err)
	}
	if latest == nil {
		t.Fatal("expected non-nil latest answer")
	}
	if latest.Version != 2 {
		t.Errorf("version = %d, want 2", latest.Version)
	}
	if latest.Body != "v2" {
		t.Errorf("body = %q, want %q", latest.Body, "v2")
	}
}

func TestVoteAnswer_ThumbsUpDown(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Thumb Room")
	q, _ := s.CreateQuestion(room.ID, "Thumbable", "v1")
	a, _ := s.CreateAnswer(q.ID, "An answer")

	// Thumb up from voter-1.
	err := s.VoteAnswer(a.ID, "voter-1", 1)
	if err != nil {
		t.Fatalf("VoteAnswer up: %v", err)
	}
	got, _ := s.GetAnswerByID(a.ID)
	if got.ThumbsUp != 1 {
		t.Errorf("thumbs_up = %d, want 1", got.ThumbsUp)
	}
	if got.ThumbsDown != 0 {
		t.Errorf("thumbs_down = %d, want 0", got.ThumbsDown)
	}

	// Thumb down from voter-2.
	s.VoteAnswer(a.ID, "voter-2", -1)
	got, _ = s.GetAnswerByID(a.ID)
	if got.ThumbsUp != 1 {
		t.Errorf("thumbs_up = %d, want 1", got.ThumbsUp)
	}
	if got.ThumbsDown != 1 {
		t.Errorf("thumbs_down = %d, want 1", got.ThumbsDown)
	}
}

func TestVoteAnswer_ChangeDirection(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Change Dir Room")
	q, _ := s.CreateQuestion(room.ID, "Changeable", "v1")
	a, _ := s.CreateAnswer(q.ID, "Answer")

	// Vote up, then change to down.
	s.VoteAnswer(a.ID, "voter-1", 1)
	s.VoteAnswer(a.ID, "voter-1", -1)

	got, _ := s.GetAnswerByID(a.ID)
	if got.ThumbsUp != 0 {
		t.Errorf("thumbs_up = %d, want 0 after direction change", got.ThumbsUp)
	}
	if got.ThumbsDown != 1 {
		t.Errorf("thumbs_down = %d, want 1 after direction change", got.ThumbsDown)
	}

	// Verify stored direction.
	dir, _ := s.GetAnswerVote(a.ID, "voter-1")
	if dir != -1 {
		t.Errorf("stored direction = %d, want -1", dir)
	}
}

func TestUnvoteAnswer(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Unvote Ans Room")
	q, _ := s.CreateQuestion(room.ID, "Unvoteable Ans", "v1")
	a, _ := s.CreateAnswer(q.ID, "Answer")

	s.VoteAnswer(a.ID, "voter-1", 1)

	err := s.UnvoteAnswer(a.ID, "voter-1")
	if err != nil {
		t.Fatalf("UnvoteAnswer: %v", err)
	}

	got, _ := s.GetAnswerByID(a.ID)
	if got.ThumbsUp != 0 {
		t.Errorf("thumbs_up = %d, want 0 after unvote", got.ThumbsUp)
	}

	dir, _ := s.GetAnswerVote(a.ID, "voter-1")
	if dir != 0 {
		t.Errorf("stored direction = %d, want 0 after unvote", dir)
	}
}

func TestUnvoteAnswer_NotFound(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Unvote NF Ans Room")
	q, _ := s.CreateQuestion(room.ID, "Q", "v1")
	a, _ := s.CreateAnswer(q.ID, "A")

	err := s.UnvoteAnswer(a.ID, "nobody")
	if err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetAnswerVote_NoVote(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Get Vote Room")
	q, _ := s.CreateQuestion(room.ID, "Q", "v1")
	a, _ := s.CreateAnswer(q.ID, "A")

	dir, err := s.GetAnswerVote(a.ID, "nobody")
	if err != nil {
		t.Fatalf("GetAnswerVote: %v", err)
	}
	if dir != 0 {
		t.Errorf("direction = %d, want 0 for no vote", dir)
	}
}

// --- Media tests ---

func TestCreateMedia_Question(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Media Room")
	q, _ := s.CreateQuestion(room.ID, "With media", "v1")

	m, err := s.CreateMedia("question", q.ID, "image", "photo.jpg", "1/q/1/abc.jpg", "image/jpeg", 12345)
	if err != nil {
		t.Fatalf("CreateMedia: %v", err)
	}
	if m.Kind != "image" {
		t.Errorf("kind = %q", m.Kind)
	}
	if m.Filename != "photo.jpg" {
		t.Errorf("filename = %q", m.Filename)
	}
	if m.MimeType != "image/jpeg" {
		t.Errorf("mime_type = %q", m.MimeType)
	}
	if m.SizeBytes != 12345 {
		t.Errorf("size_bytes = %d", m.SizeBytes)
	}
	if m.URL != "/media/1/q/1/abc.jpg" {
		t.Errorf("url = %q", m.URL)
	}
}

func TestCreateMedia_Answer(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Ans Media Room")
	q, _ := s.CreateQuestion(room.ID, "Q", "v1")
	a, _ := s.CreateAnswer(q.ID, "Answer")

	m, err := s.CreateMedia("answer", a.ID, "audio", "recording.mp3", "1/a/1/def.mp3", "audio/mpeg", 99999)
	if err != nil {
		t.Fatalf("CreateMedia answer: %v", err)
	}
	if m.Kind != "audio" {
		t.Errorf("kind = %q", m.Kind)
	}
}

func TestListMedia(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("List Media Room")
	q, _ := s.CreateQuestion(room.ID, "Q", "v1")

	s.CreateMedia("question", q.ID, "image", "a.jpg", "1/q/1/a.jpg", "image/jpeg", 100)
	s.CreateMedia("question", q.ID, "image", "b.jpg", "1/q/1/b.jpg", "image/png", 200)

	media, err := s.ListMedia("question", q.ID)
	if err != nil {
		t.Fatalf("ListMedia: %v", err)
	}
	if len(media) != 2 {
		t.Fatalf("len = %d, want 2", len(media))
	}
	if media[0].Filename != "a.jpg" {
		t.Errorf("first = %q, want %q", media[0].Filename, "a.jpg")
	}
	if media[1].Filename != "b.jpg" {
		t.Errorf("second = %q, want %q", media[1].Filename, "b.jpg")
	}
}

func TestListMedia_Empty(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Empty Media Room")
	q, _ := s.CreateQuestion(room.ID, "Q", "v1")

	media, err := s.ListMedia("question", q.ID)
	if err != nil {
		t.Fatalf("ListMedia: %v", err)
	}
	if media != nil {
		t.Errorf("expected nil, got %d media", len(media))
	}
}

func TestGetMedia(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Get Media Room")
	q, _ := s.CreateQuestion(room.ID, "Q", "v1")

	diskPath := "1/q/1/lookup.jpg"
	s.CreateMedia("question", q.ID, "image", "lookup.jpg", diskPath, "image/jpeg", 500)

	got, err := s.GetMedia(diskPath)
	if err != nil {
		t.Fatalf("GetMedia: %v", err)
	}
	if got.Filename != "lookup.jpg" {
		t.Errorf("filename = %q", got.Filename)
	}
	if got.URL != "/media/"+diskPath {
		t.Errorf("url = %q", got.URL)
	}
}

func TestGetMedia_NotFound(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	_, err := s.GetMedia("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent media")
	}
}

// --- ListQuestionsFiltered tests ---

func TestListQuestionsFiltered_Unanswered(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Filter Room")
	q1, _ := s.CreateQuestion(room.ID, "Unanswered", "v1")
	q2, _ := s.CreateQuestion(room.ID, "Will be answered", "v2")

	s.CreateAnswer(q2.ID, "Answer to q2")

	questions, err := s.ListQuestionsFiltered(room.ID, "unanswered", "")
	if err != nil {
		t.Fatalf("ListQuestionsFiltered: %v", err)
	}
	if len(questions) != 1 {
		t.Fatalf("len = %d, want 1", len(questions))
	}
	if questions[0].ID != q1.ID {
		t.Errorf("got ID %d, want %d", questions[0].ID, q1.ID)
	}
}

func TestListQuestionsFiltered_All(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Filter All Room")
	s.CreateQuestion(room.ID, "Q1", "v1")
	q2, _ := s.CreateQuestion(room.ID, "Q2", "v2")
	s.CreateAnswer(q2.ID, "Answer")

	questions, err := s.ListQuestionsFiltered(room.ID, "", "")
	if err != nil {
		t.Fatalf("ListQuestionsFiltered all: %v", err)
	}
	if len(questions) != 2 {
		t.Fatalf("len = %d, want 2", len(questions))
	}
}

func TestListQuestionsFiltered_Newest(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Filter Newest Room")
	s.CreateQuestion(room.ID, "Q1", "v1")
	q2, _ := s.CreateQuestion(room.ID, "Q2", "v2")

	questions, err := s.ListQuestionsFiltered(room.ID, "", "newest")
	if err != nil {
		t.Fatalf("ListQuestionsFiltered newest: %v", err)
	}
	if len(questions) != 2 {
		t.Fatalf("len = %d, want 2", len(questions))
	}
	if questions[0].ID != q2.ID {
		t.Errorf("first question ID = %d, want %d (newest)", questions[0].ID, q2.ID)
	}
}

// --- Answer thumb counts correctness ---

func TestAnswerThumbCounts_MultipleVoters(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Multi Thumb Room")
	q, _ := s.CreateQuestion(room.ID, "Q", "v1")
	a, _ := s.CreateAnswer(q.ID, "Answer")

	s.VoteAnswer(a.ID, "alice", 1)
	s.VoteAnswer(a.ID, "bob", 1)
	s.VoteAnswer(a.ID, "charlie", -1)

	got, _ := s.GetAnswerByID(a.ID)
	if got.ThumbsUp != 2 {
		t.Errorf("thumbs_up = %d, want 2", got.ThumbsUp)
	}
	if got.ThumbsDown != 1 {
		t.Errorf("thumbs_down = %d, want 1", got.ThumbsDown)
	}

	// Bob changes from up to down.
	s.VoteAnswer(a.ID, "bob", -1)
	got, _ = s.GetAnswerByID(a.ID)
	if got.ThumbsUp != 1 {
		t.Errorf("thumbs_up = %d, want 1 after bob changed", got.ThumbsUp)
	}
	if got.ThumbsDown != 2 {
		t.Errorf("thumbs_down = %d, want 2 after bob changed", got.ThumbsDown)
	}

	// Alice removes vote.
	s.UnvoteAnswer(a.ID, "alice")
	got, _ = s.GetAnswerByID(a.ID)
	if got.ThumbsUp != 0 {
		t.Errorf("thumbs_up = %d, want 0 after alice removed", got.ThumbsUp)
	}
	if got.ThumbsDown != 2 {
		t.Errorf("thumbs_down = %d, want 2 after alice removed", got.ThumbsDown)
	}
}

// --- Question with attached media and answers ---

func TestQuestionExtras_AttachedOnGet(t *testing.T) {
	t.Parallel()
	s := openMemory(t)

	room, _ := s.CreateRoom("Extras Room")
	q, _ := s.CreateQuestion(room.ID, "With extras", "v1")
	s.CreateMedia("question", q.ID, "image", "pic.jpg", "1/q/1/pic.jpg", "image/jpeg", 100)
	s.CreateAnswer(q.ID, "Answer v1")

	got, err := s.GetQuestion(q.ID)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if len(got.Media) != 1 {
		t.Errorf("media len = %d, want 1", len(got.Media))
	}
	if len(got.Answers) != 1 {
		t.Errorf("answers len = %d, want 1", len(got.Answers))
	}
}
