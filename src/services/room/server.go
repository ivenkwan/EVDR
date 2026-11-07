package room

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ivenkwan/evdr/src/spi"
)

// Server is the Room Service HTTP front end. All stateful behaviour lives in
// the injected SPI adapter and the projection catalog; Server is a thin,
// honest translation layer: HTTP → service layer → spi.RoomSPI.
type Server struct {
	spi     spi.RoomSPI
	auth    Authenticator
	catalog *Catalog
	events  EventSink
	logger  *slog.Logger

	// MaxUploadBytes is the authoritative upload ceiling enforced on
	// multipart bodies (FR-2.1 upstream validation). 0 → 100 MiB default.
	maxUploadBytes int64
}

// Config configures a Server. All fields are required except where noted.
type Config struct {
	// SPI is the storage adapter the service programmes against. Required —
	// the service never touches a backend directly.
	SPI spi.RoomSPI
	// Auth resolves the authenticated principal of each request. Required.
	Auth Authenticator
	// Events receives audit-worthy decisions. Required (the slog sink is the
	// default wiring in cmd/roomd).
	Events EventSink
	// Logger receives operational logs. Optional; a discard logger is used
	// when nil.
	Logger *slog.Logger
	// MaxUploadBytes caps multipart upload bodies. Optional; defaults to
	// 100 MiB.
	MaxUploadBytes int64
}

// New validates cfg and returns a ready-to-serve Server.
func New(cfg Config) (*Server, error) {
	if cfg.SPI == nil {
		return nil, errors.New("room: cfg.SPI is required")
	}
	if cfg.Auth == nil {
		return nil, errors.New("room: cfg.Auth is required")
	}
	if cfg.Events == nil {
		return nil, errors.New("room: cfg.Events is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(discardWriter{}, nil))
	}
	if cfg.MaxUploadBytes <= 0 {
		cfg.MaxUploadBytes = 100 << 20 // 100 MiB
	}
	return &Server{
		spi:            cfg.SPI,
		auth:           cfg.Auth,
		catalog:        NewCatalog(),
		events:         cfg.Events,
		logger:         cfg.Logger,
		maxUploadBytes: cfg.MaxUploadBytes,
	}, nil
}

// Handler returns the fully wired http.Handler (routes + middleware).
func (s *Server) Handler() http.Handler {
	return s.middleware(s.routes())
}

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()

	// Liveness.
	mux.HandleFunc("GET /healthz", s.handleHealthz)

	// Rooms.
	mux.HandleFunc("GET /api/v1/rooms", s.handleListRooms)
	mux.HandleFunc("POST /api/v1/rooms", s.handleCreateRoom)
	mux.HandleFunc("GET /api/v1/rooms/{roomID}", s.handleGetRoom)
	mux.HandleFunc("POST /api/v1/rooms/{roomID}/seal", s.handleSealRoom)
	mux.HandleFunc("POST /api/v1/rooms/{roomID}/retention", s.handleRetention)
	mux.HandleFunc("POST /api/v1/rooms/{roomID}/export", s.handleExportRoom)

	// Grants.
	mux.HandleFunc("GET /api/v1/rooms/{roomID}/grants", s.handleListGrants)
	mux.HandleFunc("POST /api/v1/rooms/{roomID}/grants", s.handleCreateGrant)
	mux.HandleFunc("DELETE /api/v1/rooms/{roomID}/grants/{grantID}", s.handleRevokeGrant)

	// Documents.
	mux.HandleFunc("GET /api/v1/rooms/{roomID}/documents", s.handleListDocuments)
	mux.HandleFunc("POST /api/v1/rooms/{roomID}/documents", s.handleUploadDocument)
	mux.HandleFunc("GET /api/v1/rooms/{roomID}/documents/{documentID}", s.handleGetDocument)
	mux.HandleFunc("GET /api/v1/rooms/{roomID}/documents/{documentID}/versions", s.handleListVersions)

	// Public portal surface (branding only — FR-1.1).
	mux.HandleFunc("GET /api/v1/public/rooms/{roomID}/branding", s.handlePublicBranding)

	return mux
}

// middleware composes request-scoped concerns: panic recovery, request id,
// response hardening and request logging.
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")

		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("panic serving request",
					"request_id", requestID, "method", r.Method, "path", r.URL.Path, "panic", rec)
				if !sw.wroteHeader {
					http.Error(sw, `{"code":"internal","message":"internal server error"}`, http.StatusInternalServerError)
				}
			}
			s.logger.Info("http request",
				"request_id", requestID,
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote", r.RemoteAddr,
			)
		}()

		ctx := context.WithValue(r.Context(), ctxKeyRequestID{}, requestID)
		next.ServeHTTP(sw, r.WithContext(ctx))
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "room", "spi": spi.ContractVersion})
}

// ---------------------------------------------------------------------------
// request helpers
// ---------------------------------------------------------------------------

type ctxKeyRequestID struct{}

func requestID(r *http.Request) string {
	if v, ok := r.Context().Value(ctxKeyRequestID{}).(string); ok {
		return v
	}
	return ""
}

// principal authenticates the request. Failures are written and reported.
func (s *Server) principal(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	p, err := s.auth.Authenticate(r)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			s.writeError(w, r, httpErr(http.StatusUnauthorized, codeUnauthenticated, err))
			return Principal{}, false
		}
		// Fail closed: any authenticator malfunction is an error, not a 401.
		s.logger.Error("authenticator failure", "request_id", requestID(r), "error", err)
		s.writeError(w, r, internalErr(err))
		return Principal{}, false
	}
	return p, true
}

// emit logs a decision through the event sink with request context.
func (s *Server) emit(r *http.Request, p Principal, action, target, result string, detail map[string]string) {
	ev := Event{
		Action:   action,
		Target:   target,
		TenantID: p.TenantID,
		Actor:    p.Actor,
		Result:   result,
		Detail:   detail,
		At:       time.Now().UTC(),
	}
	if ev.Detail == nil {
		ev.Detail = map[string]string{}
	}
	ev.Detail["request_id"] = requestID(r)
	s.events.Emit(r.Context(), ev)
}

// writeJSON marshals v as a JSON response body.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// writeError renders the HTTP error envelope
// {"code": "...", "message": "...", "requestId": "..."}.
//
// Client-error messages (4xx) are service-generated (validation text, SPI
// sentinels) and safe to surface. Server-side and upstream failures (5xx) are
// reduced to a fixed client-facing message — the underlying error (adapter
// URLs, upstream statuses, topology) is logged server-side only (AGENTS.md
// §6: never expose connection strings or internal topology).
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, herr *httpError) {
	message := herr.Code
	if herr.Err != nil {
		message = herr.Err.Error()
	}
	if herr.Status >= http.StatusInternalServerError {
		s.logInternalError(r, herr)
		message = internalMessage(herr.Code)
	}
	writeJSON(w, herr.Status, map[string]string{
		"code":      herr.Code,
		"message":   message,
		"requestId": requestID(r),
	})
}

// internalMessage maps 5xx error codes to fixed client-facing messages.
func internalMessage(code string) string {
	switch code {
	case "upstream_timeout":
		return "upstream request timed out"
	default:
		return "internal server error"
	}
}

// logInternalError records a 5xx failure with full server-side context; the
// detail is never echoed to the client.
func (s *Server) logInternalError(r *http.Request, herr *httpError) {
	s.logger.Error("room.internal_error",
		"request_id", requestID(r),
		"code", herr.Code,
		"status", herr.Status,
		"method", r.Method,
		"path", r.URL.Path,
		"error", herr.Error(),
	)
}

// readJSON decodes a JSON body into dst, bounding it at maxBytes. It returns
// an *httpError (payload_too_large on oversize, bad_request on malformed or
// unknown fields) for the handler to write.
func readJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) *httpError {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return httpErr(http.StatusRequestEntityTooLarge, codePayloadTooLarge, err)
		}
		return httpErr(http.StatusBadRequest, codeBadRequest, err)
	}
	return nil
}

// newRequestID returns a random 32-hex-char request id.
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	return hex.EncodeToString(b[:])
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
