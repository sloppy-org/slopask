package server

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/sloppy-org/slopask/internal/store"
)

//go:embed all:static
var staticFS embed.FS

// Server holds the HTTP server state.
type Server struct {
	store      *store.Store
	broker     *sseBroker
	uploadsDir string
	router     chi.Router
}

// New creates a Server with all routes registered.
func New(st *store.Store, uploadsDir string) *Server {
	s := &Server{
		store:      st,
		broker:     newSSEBroker(),
		uploadsDir: uploadsDir,
	}
	s.router = s.routes()
	return s
}

// Start begins listening on the given address.
func (s *Server) Start(addr string) error {
	return http.ListenAndServe(addr, s.router)
}

func (s *Server) routes() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)

	// Landing page redirects to impressum for now.
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/impressum", http.StatusTemporaryRedirect)
	})
	r.Get("/health", s.handleHealth)
	r.Get("/impressum", s.handleImpressum)

	// Serve embedded static assets at /static/.
	staticSub, _ := fs.Sub(staticFS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Serve uploaded media files.
	r.Get("/media/*", s.handleMedia)

	// Student routes.
	r.Route("/r/{slug}", func(r chi.Router) {
		r.Get("/", s.handleRoom)
		r.Get("/questions", s.handleListQuestions)
		r.Post("/questions", s.handleCreateQuestion)
		r.Post("/questions/{qid}/vote", s.handleVote)
		r.Delete("/questions/{qid}/vote", s.handleUnvote)
		r.Post("/answers/{aid}/vote", s.handleVoteAnswer)
		r.Get("/events", s.handleStudentSSE)
	})

	// Admin routes.
	r.Route("/admin/{token}", func(r chi.Router) {
		r.Get("/", s.handleAdmin)
		r.Get("/questions", s.handleAdminListQuestions)
		r.Put("/questions/{qid}", s.handleUpdateQuestion)
		r.Post("/questions/{qid}/answer", s.handleCreateAnswer)
		r.Delete("/questions/{qid}", s.handleDeleteQuestion)
		r.Delete("/media/question/{mid}", s.handleDeleteMedia)
		r.Delete("/media/answer/{mid}", s.handleDeleteMedia)
		r.Get("/events", s.handleAdminSSE)
	})

	// External API (slopcast integration).
	r.Route("/api/v0/rooms/{token}", func(r chi.Router) {
		r.Get("/questions", s.handleAPIListQuestions)
		r.Get("/questions/{qid}", s.handleAPIGetQuestion)
		r.Put("/questions/{qid}", s.handleUpdateQuestion)
		r.Post("/questions/{qid}/answer", s.handleAPICreateAnswer)
	})

	return r
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
