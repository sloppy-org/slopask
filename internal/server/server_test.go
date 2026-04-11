package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sloppy-org/slopask/internal/store"
)

// newTestServer creates a test HTTP server backed by an in-memory store.
func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	uploadsDir := t.TempDir()
	srv := New(st, uploadsDir)
	ts := httptest.NewServer(srv.router)
	t.Cleanup(func() {
		ts.Close()
		st.Close()
	})
	return ts, st
}

// createTestRoom is a helper that creates a room directly via the store.
func createTestRoom(t *testing.T, st *store.Store, title string) *store.Room {
	t.Helper()
	room, err := st.CreateRoom(title)
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	return room
}

// createTestQuestion is a helper that creates a question directly via the store.
func createTestQuestion(t *testing.T, st *store.Store, roomID int64, body string) *store.Question {
	t.Helper()
	q, err := st.CreateQuestion(roomID, body, "test-voter")
	if err != nil {
		t.Fatalf("create question: %v", err)
	}
	return q
}

// --- Student endpoints ---

func TestHandleRoom_OK(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "Test Room")

	resp, err := http.Get(ts.URL + "/r/" + room.Slug)
	if err != nil {
		t.Fatalf("GET /r/{slug}: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("content-type = %q", ct)
	}
}

func TestHandleRoom_NotFound(t *testing.T) {
	t.Parallel()
	ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/r/nonexistent")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHandleListQuestions(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "List Q Room")
	createTestQuestion(t, st, room.ID, "Question 1")
	createTestQuestion(t, st, room.ID, "Question 2")

	resp, err := http.Get(ts.URL + "/r/" + room.Slug + "/questions")
	if err != nil {
		t.Fatalf("GET questions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var questions []store.Question
	if err := json.NewDecoder(resp.Body).Decode(&questions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(questions) != 2 {
		t.Errorf("len = %d, want 2", len(questions))
	}
}

func TestHandleListQuestions_EmptyRoom(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "Empty Room")

	resp, err := http.Get(ts.URL + "/r/" + room.Slug + "/questions")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var questions []store.Question
	json.NewDecoder(resp.Body).Decode(&questions)
	if len(questions) != 0 {
		t.Errorf("len = %d, want 0", len(questions))
	}
}

func TestHandleCreateQuestion(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "Create Q Room")

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	w.WriteField("body", "How does this work?")
	w.WriteField("voter_id", "student-1")
	w.Close()

	resp, err := http.Post(ts.URL+"/r/"+room.Slug+"/questions", w.FormDataContentType(), body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 201, body: %s", resp.StatusCode, b)
	}
	var q store.Question
	json.NewDecoder(resp.Body).Decode(&q)
	if q.Body != "How does this work?" {
		t.Errorf("body = %q", q.Body)
	}
	if q.ID == 0 {
		t.Error("question ID is 0")
	}
}

func TestHandleCreateQuestion_EmptyBody(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "Empty Body Room")

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	w.WriteField("body", "")
	w.Close()

	resp, err := http.Post(ts.URL+"/r/"+room.Slug+"/questions", w.FormDataContentType(), body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleVote(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "Vote Room")
	q := createTestQuestion(t, st, room.ID, "Voteable")

	payload := fmt.Sprintf(`{"voter_id":"voter-1"}`)
	resp, err := http.Post(
		ts.URL+"/r/"+room.Slug+fmt.Sprintf("/questions/%d/vote", q.ID),
		"application/json",
		bytes.NewBufferString(payload),
	)
	if err != nil {
		t.Fatalf("POST vote: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200, body: %s", resp.StatusCode, b)
	}

	var result map[string]int64
	json.NewDecoder(resp.Body).Decode(&result)
	if result["vote_count"] != 1 {
		t.Errorf("vote_count = %d, want 1", result["vote_count"])
	}
}

func TestHandleVote_Duplicate(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "Dup Vote Room")
	q := createTestQuestion(t, st, room.ID, "Q")

	url := ts.URL + "/r/" + room.Slug + fmt.Sprintf("/questions/%d/vote", q.ID)
	payload := `{"voter_id":"voter-1"}`

	resp1, _ := http.Post(url, "application/json", bytes.NewBufferString(payload))
	resp1.Body.Close()

	resp2, err := http.Post(url, "application/json", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 409 {
		t.Errorf("status = %d, want 409 (conflict)", resp2.StatusCode)
	}
}

func TestHandleUnvote(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "Unvote Room")
	q := createTestQuestion(t, st, room.ID, "Unvoteable")
	st.Vote(q.ID, "voter-1")

	url := ts.URL + "/r/" + room.Slug + fmt.Sprintf("/questions/%d/vote", q.ID)
	req, _ := http.NewRequest(http.MethodDelete, url, bytes.NewBufferString(`{"voter_id":"voter-1"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200, body: %s", resp.StatusCode, b)
	}

	var result map[string]int64
	json.NewDecoder(resp.Body).Decode(&result)
	if result["vote_count"] != 0 {
		t.Errorf("vote_count = %d, want 0", result["vote_count"])
	}
}

func TestHandleVoteAnswer(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "Vote Ans Room")
	q := createTestQuestion(t, st, room.ID, "Q")
	a, err := st.CreateAnswer(q.ID, "The answer")
	if err != nil {
		t.Fatalf("create answer: %v", err)
	}

	url := ts.URL + "/r/" + room.Slug + fmt.Sprintf("/answers/%d/vote", a.ID)
	payload := `{"voter_id":"student-1","direction":1}`
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200, body: %s", resp.StatusCode, b)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["thumbs_up"].(float64) != 1 {
		t.Errorf("thumbs_up = %v, want 1", result["thumbs_up"])
	}
}

func TestHandleVoteAnswer_ThumbsDown(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "Thumb Down Room")
	q := createTestQuestion(t, st, room.ID, "Q")
	a, _ := st.CreateAnswer(q.ID, "Answer")

	url := ts.URL + "/r/" + room.Slug + fmt.Sprintf("/answers/%d/vote", a.ID)
	payload := `{"voter_id":"student-1","direction":-1}`
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["thumbs_down"].(float64) != 1 {
		t.Errorf("thumbs_down = %v, want 1", result["thumbs_down"])
	}
}

func TestHandleVoteAnswer_RemoveVote(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "Remove Vote Room")
	q := createTestQuestion(t, st, room.ID, "Q")
	a, _ := st.CreateAnswer(q.ID, "Answer")
	st.VoteAnswer(a.ID, "student-1", 1)

	url := ts.URL + "/r/" + room.Slug + fmt.Sprintf("/answers/%d/vote", a.ID)
	payload := `{"voter_id":"student-1","direction":0}`
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["thumbs_up"].(float64) != 0 {
		t.Errorf("thumbs_up = %v, want 0", result["thumbs_up"])
	}
}

// --- Admin endpoints ---

func TestHandleAdmin_OK(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "Admin Room")

	resp, err := http.Get(ts.URL + "/admin/" + room.AdminToken)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("content-type = %q", ct)
	}
}

func TestHandleAdmin_NotFound(t *testing.T) {
	t.Parallel()
	ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/admin/badtoken")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHandleCreateAnswer(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "Create Ans Room")
	q := createTestQuestion(t, st, room.ID, "Answerable")

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	w.WriteField("body", "This is the answer")
	w.Close()

	url := ts.URL + "/admin/" + room.AdminToken + fmt.Sprintf("/questions/%d/answer", q.ID)
	resp, err := http.Post(url, w.FormDataContentType(), body)
	if err != nil {
		t.Fatalf("POST answer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 201, body: %s", resp.StatusCode, b)
	}

	var a store.Answer
	json.NewDecoder(resp.Body).Decode(&a)
	if a.Version != 1 {
		t.Errorf("version = %d, want 1", a.Version)
	}
	if a.Body != "This is the answer" {
		t.Errorf("body = %q", a.Body)
	}
}

func TestHandleCreateAnswer_SecondVersion(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "Version Room")
	q := createTestQuestion(t, st, room.ID, "Multi-version")

	postAnswer := func(text string) store.Answer {
		t.Helper()
		body := &bytes.Buffer{}
		w := multipart.NewWriter(body)
		w.WriteField("body", text)
		w.Close()
		url := ts.URL + "/admin/" + room.AdminToken + fmt.Sprintf("/questions/%d/answer", q.ID)
		resp, err := http.Post(url, w.FormDataContentType(), body)
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		var a store.Answer
		json.NewDecoder(resp.Body).Decode(&a)
		return a
	}

	a1 := postAnswer("First")
	if a1.Version != 1 {
		t.Errorf("v1: version = %d", a1.Version)
	}

	a2 := postAnswer("Second")
	if a2.Version != 2 {
		t.Errorf("v2: version = %d", a2.Version)
	}
}

func TestHandleDeleteQuestion(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "Delete Q Room")
	q := createTestQuestion(t, st, room.ID, "Delete me")

	url := ts.URL + "/admin/" + room.AdminToken + fmt.Sprintf("/questions/%d", q.ID)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}

	// Verify question is gone.
	questions, _ := st.ListQuestions(room.ID, "")
	if len(questions) != 0 {
		t.Errorf("questions still present after delete: %d", len(questions))
	}
}

// --- API endpoints ---

func TestAPIListQuestions(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "API List Room")
	createTestQuestion(t, st, room.ID, "Q1")
	createTestQuestion(t, st, room.ID, "Q2")

	resp, err := http.Get(ts.URL + "/api/v0/rooms/" + room.AdminToken + "/questions")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var questions []store.Question
	json.NewDecoder(resp.Body).Decode(&questions)
	if len(questions) != 2 {
		t.Errorf("len = %d, want 2", len(questions))
	}
}

func TestAPIListQuestions_Unanswered(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "API Filter Room")
	createTestQuestion(t, st, room.ID, "Unanswered")
	q2 := createTestQuestion(t, st, room.ID, "Answered")
	st.CreateAnswer(q2.ID, "Answer")

	resp, err := http.Get(ts.URL + "/api/v0/rooms/" + room.AdminToken + "/questions?filter=unanswered")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var questions []store.Question
	json.NewDecoder(resp.Body).Decode(&questions)
	if len(questions) != 1 {
		t.Errorf("len = %d, want 1", len(questions))
	}
}

func TestAPIListQuestions_BadToken(t *testing.T) {
	t.Parallel()
	ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/v0/rooms/badtoken/questions")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestAPIGetQuestion(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "API Get Room")
	q := createTestQuestion(t, st, room.ID, "Detail question")

	resp, err := http.Get(ts.URL + "/api/v0/rooms/" + room.AdminToken + fmt.Sprintf("/questions/%d", q.ID))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var got store.Question
	json.NewDecoder(resp.Body).Decode(&got)
	if got.Body != "Detail question" {
		t.Errorf("body = %q", got.Body)
	}
}

func TestAPICreateAnswer(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "API Create Ans Room")
	q := createTestQuestion(t, st, room.ID, "API answerable")

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	w.WriteField("body", "API answer")
	w.Close()

	url := ts.URL + "/api/v0/rooms/" + room.AdminToken + fmt.Sprintf("/questions/%d/answer", q.ID)
	resp, err := http.Post(url, w.FormDataContentType(), body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 201, body: %s", resp.StatusCode, b)
	}
	var a store.Answer
	json.NewDecoder(resp.Body).Decode(&a)
	if a.Version != 1 {
		t.Errorf("version = %d, want 1", a.Version)
	}
	if a.Body != "API answer" {
		t.Errorf("body = %q", a.Body)
	}
}

// --- Static/utility endpoints ---

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()
	ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "ok" {
		t.Errorf("status = %q, want %q", result["status"], "ok")
	}
}

func TestImpressumEndpoint(t *testing.T) {
	t.Parallel()
	ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/legalnotes")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestMediaEndpoint_NotFound(t *testing.T) {
	t.Parallel()
	ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/media/nonexistent")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	// httptest serves a 404 for nonexistent files.
	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()
	ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Errorf("X-Frame-Options = %q", resp.Header.Get("X-Frame-Options"))
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", resp.Header.Get("X-Content-Type-Options"))
	}
	if resp.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("Referrer-Policy = %q", resp.Header.Get("Referrer-Policy"))
	}
}

func TestRootRedirect(t *testing.T) {
	t.Parallel()
	ts, _ := newTestServer(t)

	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 307 {
		t.Errorf("status = %d, want 307", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/legalnotes" {
		t.Errorf("location = %q, want /legalnotes", loc)
	}
}

func TestHandleVoteAnswer_InvalidDirection(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "Invalid Dir Room")
	q := createTestQuestion(t, st, room.ID, "Q")
	a, _ := st.CreateAnswer(q.ID, "Answer")

	url := ts.URL + "/r/" + room.Slug + fmt.Sprintf("/answers/%d/vote", a.ID)
	payload := `{"voter_id":"student-1","direction":5}`
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAdminListQuestions(t *testing.T) {
	t.Parallel()
	ts, st := newTestServer(t)
	room := createTestRoom(t, st, "Admin List Room")
	createTestQuestion(t, st, room.ID, "Q1")

	resp, err := http.Get(ts.URL + "/admin/" + room.AdminToken + "/questions")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var questions []store.Question
	json.NewDecoder(resp.Body).Decode(&questions)
	if len(questions) != 1 {
		t.Errorf("len = %d, want 1", len(questions))
	}
}
