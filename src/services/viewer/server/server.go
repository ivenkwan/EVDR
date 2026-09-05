// Package server exposes the page-streaming HTTP API of the EVDR secure
// viewer (FR-3.1, FR-3.2, TR-4.1, TR-4.2).
//
// # Endpoints
//
//	GET /v1/healthz                                   liveness
//	GET /v1/viewer/rooms/{roomID}/documents/{documentID}/pages/{page}
//	    → one rendered, server-watermarked page (never the whole document)
//	GET /v1/viewer/rooms/{roomID}/documents/{documentID}/pages
//	    → {"pageCount": N} — metadata only, no content crosses the HTTP
//	      boundary. There is deliberately NO whole-document endpoint.
//
// # Identity and tenancy
//
// The server is bound to exactly one tenant at construction (Config.TenantID),
// mirroring the per-tenant adapter topology (TR-2.2). The principal is taken
// from an authenticated header (X-EVDR-Principal) that the platform's
// Keycloak/JWT gateway middleware will populate in front of this service —
// a tenant_id is NEVER read from the client, and a tenant header is not even
// accepted (SR-2.2). spi.TenantContext is built from config + verified
// principal only.
//
// # Access control
//
// Every page request is resolved through spi.RoomSPI.GetRenderStream, which
// enforces the actor's grant on the document's owning room before any content
// is fetched. SPI errors are mapped to HTTP statuses without leaking
// internals (see mapError).
//
// # Watermark tokens
//
// The server resolves FR-1.3 tokens (viewer identity, timestamp, client
// IP/domain, session ID) from the request context into spi.WatermarkSpec and
// passes them to the SPI, whose contract bakes the watermark into the page
// server-side (FR-3.2). Byte-level watermarking is implemented by the viewer
// render pipeline (package viewer/watermark) wired into the adapter; the HTTP
// layer never trusts the client with watermarking.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ivenkwan/evdr/src/spi"
)

// headerPrincipal is the gateway-authenticated principal header. A future
// Keycloak/JWT middleware (or the gateway itself) populates it; until then a
// direct deployment is expected to be reachable only inside the trusted
// network.
const headerPrincipal = "X-EVDR-Principal"

// headerActorKind optionally overrides the actor kind; defaults to
// internal_user. Guests and service actors are honoured for grant checks.
const headerActorKind = "X-EVDR-Actor"

// headerDisplayName optionally carries the principal's display name.
const headerDisplayName = "X-EVDR-Display-Name"

// headerSession optionally carries the viewer session id (FR-1.3 token).
const headerSession = "X-EVDR-Session"

// Preset is the service-level watermark preset (FR-3.6/TR-4.6 placeholder:
// per-room presets arrive with the policy engine; the service applies its
// configured preset to every render request it issues).
type Preset struct {
	Density  string  // sparse | normal | dense
	Opacity  float64 // 0.0–1.0
	Rotation float64 // degrees
}

// DefaultPreset returns the platform-default watermark preset.
func DefaultPreset() Preset {
	return Preset{Density: "normal", Opacity: 0.4, Rotation: 45}
}

// Config configures the viewer HTTP server. TenantID and SPI are required.
type Config struct {
	// TenantID is the single tenant this service instance serves (TR-2.2).
	TenantID spi.TenantID
	// SPI is the Room SPI the server resolves every page through.
	SPI spi.RoomSPI
	// Logger is the structured log sink; slog.Default() when nil.
	Logger *slog.Logger
	// TrustProxy makes the server honour X-Forwarded-For for the client-IP
	// watermark token. Enable only when Traefik/gateway terminates the
	// connection and overwrites the header (see docs/ADR/0002).
	TrustProxy bool
	// Watermark is the preset applied to every render request.
	Watermark Preset
}

// Server is the viewer HTTP handler. It is stateless after New and safe for
// concurrent use.
type Server struct {
	cfg Config
	log *slog.Logger
}

// New validates cfg and returns a ready-to-serve viewer.
func New(cfg Config) (*Server, error) {
	if cfg.TenantID == "" {
		return nil, errors.New("server: TenantID is required")
	}
	if cfg.SPI == nil {
		return nil, errors.New("server: SPI is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Watermark.Density == "" {
		cfg.Watermark = DefaultPreset()
	}
	return &Server{cfg: cfg, log: cfg.Logger}, nil
}

// Routes returns the fully wired http.Handler.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/healthz", s.handleHealthz)
	mux.HandleFunc("GET /v1/viewer/rooms/{roomID}/documents/{documentID}/pages/{page}", s.handlePage)
	mux.HandleFunc("GET /v1/viewer/rooms/{roomID}/documents/{documentID}/pages", s.handlePageCount)
	return mux
}

// handleHealthz reports liveness. Readiness (SPI reachability) is a
// deployment concern; the SPI has no health method.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "evdr-viewer",
		"tenant":  string(s.cfg.TenantID),
	})
}

// handlePage serves exactly one rendered page for document {documentID}. The
// request is a single-page RenderRequest (FirstPage == LastPage == {page}) so
// the SPI's view-scoped contract guarantees the full document never crosses
// this boundary (FR-3.1). The optional ?version= query selects an immutable
// version (zero = current).
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenant, identity, err := s.identity(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	roomID := spi.RoomID(r.PathValue("roomID"))
	documentID := spi.DocumentID(r.PathValue("documentID"))
	pageNo, err := strconv.Atoi(r.PathValue("page"))
	if err != nil || pageNo < 1 {
		// An invalid page is a client error, not a not-found.
		s.logError("viewer.bad_request", identity, roomID, documentID, "invalid page number", nil)
		s.writeError(w, r, errBadRequest("page must be a positive integer"))
		return
	}
	version := spi.VersionID(r.URL.Query().Get("version"))
	spec := s.watermarkSpec(r, identity)

	stream, err := s.cfg.SPI.GetRenderStream(ctx, tenant, documentID, spi.RenderRequest{
		Version:   version,
		FirstPage: pageNo,
		LastPage:  pageNo,
		Watermark: spec,
	})
	if err != nil {
		s.logError("viewer.page_denied", identity, roomID, documentID, err.Error(), err)
		s.writeError(w, r, err)
		return
	}
	defer stream.Close()

	if !stream.Next() {
		if streamErr := stream.Err(); streamErr != nil {
			s.writeError(w, r, streamErr)
			return
		}
		// The range is empty: the document (version) has no such page.
		s.writeError(w, r, errPageNotFound(pageNo))
		return
	}
	page := stream.Page()
	// Enforce the single-page invariant: an adapter that streams more than
	// the requested page has violated the SPI contract — fail closed before
	// writing any page bytes.
	if stream.Next() {
		s.logError("viewer.contract_violation", identity, roomID, documentID,
			"render stream delivered more pages than requested", spi.ErrUnsupported)
		s.writeError(w, r, errors.New("viewer: render stream contract violation"))
		return
	}

	w.Header().Set("Content-Type", contentTypeOf(page.ContentType))
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-EVDR-Page", strconv.Itoa(pageNo))
	w.Header().Set("X-EVDR-Tenant", string(tenant.TenantID))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, page.Content); err != nil {
		// The client went away mid-stream; nothing more to serve.
		s.logError("viewer.page_aborted", identity, roomID, documentID, err.Error(), err)
		return
	}
	s.log.Info("viewer.page_served",
		"tenant_id", string(tenant.TenantID),
		"actor", identity.ID,
		"room_id", string(roomID),
		"document_id", string(documentID),
		"page", pageNo,
		"session_id", identity.SessionID,
		"client_ip", identity.ClientIP,
		"content_type", page.ContentType,
		"result", "ok")
}

// handlePageCount reports how many pages a document has — metadata only. The
// content of every page is discarded inside the service; only the count
// crosses the HTTP boundary (FR-3.1).
func (s *Server) handlePageCount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenant, identity, err := s.identity(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	roomID := spi.RoomID(r.PathValue("roomID"))
	documentID := spi.DocumentID(r.PathValue("documentID"))
	version := spi.VersionID(r.URL.Query().Get("version"))

	stream, err := s.cfg.SPI.GetRenderStream(ctx, tenant, documentID, spi.RenderRequest{
		Version:   version,
		FirstPage: 1,
		LastPage:  math.MaxInt32,
		Watermark: s.watermarkSpec(r, identity),
	})
	if err != nil {
		s.logError("viewer.pagecount_denied", identity, roomID, documentID, err.Error(), err)
		s.writeError(w, r, err)
		return
	}
	defer stream.Close()

	count := 0
	for stream.Next() {
		count++
	}
	if streamErr := stream.Err(); streamErr != nil {
		s.writeError(w, r, streamErr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"document_id": string(documentID), "pageCount": count})
	s.log.Info("viewer.pagecount_served",
		"tenant_id", string(tenant.TenantID),
		"actor", identity.ID,
		"room_id", string(roomID),
		"document_id", string(documentID),
		"page_count", count,
		"result", "ok")
}

// identity resolves the spi.TenantContext for one request: the config-bound
// tenant plus the gateway-authenticated principal header. Client-supplied
// tenant parameters are never consulted (SR-2.2).
func (s *Server) identity(r *http.Request) (spi.TenantContext, identityInfo, error) {
	principal := strings.TrimSpace(r.Header.Get(headerPrincipal))
	if principal == "" {
		return spi.TenantContext{}, identityInfo{}, errUnauthorized("missing " + headerPrincipal + " header")
	}
	kind := spi.ActorInternalUser
	switch strings.ToLower(strings.TrimSpace(r.Header.Get(headerActorKind))) {
	case "", "internal_user":
		kind = spi.ActorInternalUser
	case "guest":
		kind = spi.ActorGuest
	case "service":
		kind = spi.ActorService
	default:
		return spi.TenantContext{}, identityInfo{}, errBadRequest("invalid " + headerActorKind + " value")
	}
	info := identityInfo{
		ID:          principal,
		Kind:        kind,
		DisplayName: strings.TrimSpace(r.Header.Get(headerDisplayName)),
		SessionID:   strings.TrimSpace(r.Header.Get(headerSession)),
		ClientIP:    s.clientIP(r),
	}
	tenant := spi.TenantContext{
		TenantID: s.cfg.TenantID,
		Actor: spi.Actor{
			Kind:        kind,
			ID:          info.ID,
			DisplayName: info.DisplayName,
		},
	}
	return tenant, info, nil
}

// identityInfo is the per-request principal context used for watermark
// tokens and audit logging.
type identityInfo struct {
	ID          string
	Kind        spi.ActorKind
	DisplayName string
	SessionID   string
	ClientIP    string
}

// clientIP resolves the client address for the watermark token: RemoteAddr by
// default, or the leftmost X-Forwarded-For entry when the gateway is trusted.
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if first, _, ok := strings.Cut(xff, ","); ok {
				return strings.TrimSpace(first)
			}
			return strings.TrimSpace(xff)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// watermarkSpec resolves the FR-1.3 tokens from the request context and the
// configured preset. The SPI contract bakes the spec into the rendered page
// server-side (FR-3.2).
func (s *Server) watermarkSpec(r *http.Request, id identityInfo) spi.WatermarkSpec {
	return spi.WatermarkSpec{
		ViewerIdentity: id.ID,
		Timestamp:      time.Now().UTC(),
		ClientIP:       id.ClientIP,
		SessionID:      id.SessionID,
		Density:        s.cfg.Watermark.Density,
		Opacity:        s.cfg.Watermark.Opacity,
		Rotation:       s.cfg.Watermark.Rotation,
	}
}

func contentTypeOf(ct string) string {
	if ct == "" {
		return "application/octet-stream"
	}
	return ct
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errResponse is the error envelope served to clients. Messages are generic;
// detail stays in the server log.
type errResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// logError records a structured decision/error event (AGENTS.md §6: no
// silent data paths; structured logging only).
func (s *Server) logError(event string, id identityInfo, roomID spi.RoomID, docID spi.DocumentID, detail string, cause error) {
	args := []any{
		"tenant_id", string(s.cfg.TenantID),
		"actor", id.ID,
		"actor_kind", string(id.Kind),
		"room_id", string(roomID),
		"document_id", string(docID),
		"session_id", id.SessionID,
		"client_ip", id.ClientIP,
		"detail", detail,
		"result", "denied",
	}
	if cause != nil {
		args = append(args, "error", cause.Error())
	}
	s.log.Warn(event, args...)
}
