// Package nextcloud implements the EVDR Room SPI (src/spi) on top of a
// Nextcloud instance's OCS sharing API and WebDAV file API (TR-2.2).
//
// # Tenant binding
//
// TR-2.2 deploys one Nextcloud instance per cell/tenant, so the adapter is
// bound to exactly one tenant at construction (Config.TenantID). Calls that
// carry a different TenantContext are rejected — CreateRoom with
// spi.ErrAccessDenied, room-scoped operations with spi.ErrRoomNotFound and
// document-scoped operations with spi.ErrDocumentNotFound — so a tenant can
// never probe another tenant's existence through this adapter.
//
// # Storage layout
//
// Everything lives under the service account's WebDAV namespace below
// /remote.php/dav/files/<service-user>/evdr/:
//
//	_evdr/rooms.json                     RoomID → slug index (adapter root)
//	<slug>/                              room folder (OCS share root)
//	  docs/<folderPath>/<name>           current version of a document (live pointer)
//	  _evdr/room.json                    room record (state, retention, branding)
//	  _evdr/grants.json                  grant ledger (incl. OCS share ids)
//	  _evdr/seal.json                    seal receipt, present once sealed
//	  _evdr/docs.json                    normalized name+folder → document id index
//	  _evdr/docs/<docID>.json            per-document record (immutable version list)
//	  _evdr/versions/<docID>/v<N>        immutable version content (WebDAV COPY)
//
// Version content is archived under _evdr/versions at upload time and never
// modified afterwards; the live docs/<folderPath>/<name> file is only ever
// overwritten by PutDocument as the "current pointer". ExportRoom reads the
// archives only, so exports are consistent snapshots.
//
// # Concurrency
//
// The adapter is safe for concurrent use within one process: per-room mutexes
// serialise read-modify-write of the JSON ledgers. Cross-process writes are
// NOT serialised (Nextcloud provides no compare-and-swap on plain files); the
// deployment model assumes one Room Service process per cell, which matches
// the "one Nextcloud instance per cell/tenant" topology.
package nextcloud

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ivenkwan/evdr/src/spi"
)

// Renderer turns one document version's content into a forward-only page
// stream (spi.RenderStream). Rendering — Office→PDF conversion, page
// rasterisation, server-side watermark baking — is a viewer-pipeline concern
// (TR-4.x) and is deliberately NOT implemented inside the storage adapter:
// the platform wires its Renderer at adapter construction, and tests inject
// fakes. The adapter's job is the view-scoped transport: fetch the requested
// immutable version over WebDAV, enforce the actor's grant, and stream the
// already-rendered pages.
type Renderer interface {
	// Render renders content into the page range requested by req, with the
	// watermark baked in server-side before delivery. The returned stream
	// must unblock Next() when ctx is cancelled and surface the error via
	// Err().
	Render(ctx context.Context, content io.Reader, contentType string, req spi.RenderRequest) (spi.RenderStream, error)
}

// Config configures a NextcloudAdapter. All fields except HTTPClient and
// Renderer are required.
type Config struct {
	// BaseURL is the Nextcloud server base URL, e.g. "https://nc.example.com".
	BaseURL string
	// Username is the platform service account used for all OCS/WebDAV calls.
	Username string
	// AppPassword is the service account's app password / API token.
	AppPassword string
	// TenantID is the tenant served by this Nextcloud instance (TR-2.2).
	TenantID spi.TenantID
	// HTTPClient is optional; a client with a 60s timeout is used when nil.
	HTTPClient *http.Client
	// Renderer is the viewer-pipeline renderer used by GetRenderStream.
	// When nil, GetRenderStream returns spi.ErrUnsupported.
	Renderer Renderer
}

// Adapter implements spi.RoomSPI against a Nextcloud backend.
type Adapter struct {
	cfg        Config
	httpClient *http.Client

	mu        sync.Mutex
	roomLocks map[spi.RoomID]*sync.Mutex
}

// New validates cfg and returns a ready-to-use Adapter.
func New(cfg Config) (*Adapter, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("nextcloud: BaseURL is required")
	}
	if cfg.Username == "" {
		return nil, errors.New("nextcloud: Username is required")
	}
	if cfg.AppPassword == "" {
		return nil, errors.New("nextcloud: AppPassword is required")
	}
	if cfg.TenantID == "" {
		return nil, errors.New("nextcloud: TenantID is required")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Adapter{cfg: cfg, httpClient: cfg.HTTPClient, roomLocks: make(map[spi.RoomID]*sync.Mutex)}, nil
}

// errTenantMismatch is the internal marker for a TenantContext that does not
// match the tenant this adapter instance serves. Each method translates it
// into the contract-required sentinel (see package doc).
var errTenantMismatch = errors.New("spi: tenant context does not match adapter tenant")

// checkTenant returns errTenantMismatch when the caller's tenant context is
// not the tenant this adapter instance is bound to.
func (a *Adapter) checkTenant(tenant spi.TenantContext) error {
	if tenant.TenantID != a.cfg.TenantID {
		return errTenantMismatch
	}
	return nil
}

// scopeRoom wraps errTenantMismatch into the room-scoped not-found sentinel.
func scopeRoom(err error) error {
	if errors.Is(err, errTenantMismatch) {
		return spi.ErrRoomNotFound
	}
	return err
}

// scopeDoc wraps errTenantMismatch into the document-scoped not-found sentinel.
func scopeDoc(err error) error {
	if errors.Is(err, errTenantMismatch) {
		return spi.ErrDocumentNotFound
	}
	return err
}

// lockRoom serialises access to one room's ledgers within this process.
func (a *Adapter) lockRoom(roomID spi.RoomID) func() {
	a.mu.Lock()
	m, ok := a.roomLocks[roomID]
	if !ok {
		m = &sync.Mutex{}
		a.roomLocks[roomID] = m
	}
	a.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// newUUID returns a random RFC 4122 v4 UUID string (stdlib only).
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is unrecoverable for a compliance product;
		// panic loudly rather than mint weak identifiers.
		panic(fmt.Sprintf("nextcloud: crypto/rand failed: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// joinPath escapes and joins path segments. Slashes inside a segment are
// escaped so a filename can never widen its path.
func joinPath(segs ...string) string {
	esc := make([]string, len(segs))
	for i, s := range segs {
		esc[i] = url.PathEscape(s)
	}
	return strings.Join(esc, "/")
}

// davBase is the WebDAV namespace root for this adapter's rooms.
func (a *Adapter) davBase() string {
	return strings.TrimSuffix(a.cfg.BaseURL, "/") + "/remote.php/dav/files/" +
		url.PathEscape(a.cfg.Username) + "/evdr"
}

// davURL builds a WebDAV URL under the adapter namespace.
func (a *Adapter) davURL(segs ...string) string {
	return a.davBase() + "/" + joinPath(segs...)
}

// roomURL builds a WebDAV URL inside a room folder.
func (a *Adapter) roomURL(segs ...string) string {
	return a.davURL(segs...)
}

// ocsSharesURL is the Nextcloud OCS v2 share API endpoint.
func (a *Adapter) ocsSharesURL() string {
	return strings.TrimSuffix(a.cfg.BaseURL, "/") + "/ocs/v2.php/apps/files_sharing/api/v1/shares"
}

// basicAuth builds the HTTP Basic authorization header value.
func (a *Adapter) basicAuth() string {
	token := base64.StdEncoding.EncodeToString([]byte(a.cfg.Username + ":" + a.cfg.AppPassword))
	return "Basic " + token
}

// do issues one authenticated request. ocs controls the OCS-APIRequest header
// required by the OCS API. The caller owns the returned response body.
func (a *Adapter) do(ctx context.Context, method, url string, body io.Reader, contentType string, ocs bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("spi: %s %s: %w", method, url, err)
	}
	req.Header.Set("Authorization", a.basicAuth())
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if ocs {
		req.Header.Set("OCS-APIRequest", "true")
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("spi: %s %s: %w", method, url, err)
	}
	return resp, nil
}

// drain consumes and closes a response body we are not going to read.
func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// readJSON GETs a JSON document. Returns (false, nil) when the document does
// not exist (404); callers map absence to the contract sentinel.
func (a *Adapter) readJSON(ctx context.Context, url string, v any) (bool, error) {
	resp, err := a.do(ctx, http.MethodGet, url, nil, "", false)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			return false, fmt.Errorf("spi: decode %s: %w", url, err)
		}
		return true, nil
	case http.StatusNotFound:
		drain(resp)
		return false, nil
	case http.StatusInsufficientStorage:
		drain(resp)
		return false, spi.ErrQuotaExceeded
	default:
		drain(resp)
		return false, fmt.Errorf("spi: GET %s: unexpected status %d", url, resp.StatusCode)
	}
}

// writeJSON PUTs a JSON document.
func (a *Adapter) writeJSON(ctx context.Context, url string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("spi: marshal %s: %w", url, err)
	}
	resp, err := a.do(ctx, http.MethodPut, url, bytesReader(b), "application/json", false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusNoContent, http.StatusOK:
		return nil
	case http.StatusInsufficientStorage:
		drain(resp)
		return spi.ErrQuotaExceeded
	default:
		drain(resp)
		return fmt.Errorf("spi: PUT %s: unexpected status %d", url, resp.StatusCode)
	}
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// decodeXML decodes one XML document from r.
func decodeXML(r io.Reader, v any) error {
	dec := xml.NewDecoder(r)
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("spi: decode XML: %w", err)
	}
	return nil
}

// mkcol creates a WebDAV collection. Existing collections (405) are not an
// error; created (201) and no-content (204) are success.
func (a *Adapter) mkcol(ctx context.Context, url string) error {
	resp, err := a.do(ctx, "MKCOL", url, nil, "", false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusNoContent, http.StatusMethodNotAllowed:
		drain(resp)
		return nil
	case http.StatusInsufficientStorage:
		drain(resp)
		return spi.ErrQuotaExceeded
	default:
		drain(resp)
		return fmt.Errorf("spi: MKCOL %s: unexpected status %d", url, resp.StatusCode)
	}
}

// ocsResponse is the subset of the Nextcloud OCS API response envelope we use.
type ocsResponse struct {
	Meta struct {
		Status     string `xml:"status"`
		StatusCode int    `xml:"statuscode"`
		Message    string `xml:"message"`
	} `xml:"meta"`
	Data struct {
		ID    int    `xml:"id"`
		URL   string `xml:"url"`
		Token string `xml:"token"`
	} `xml:"data"`
}

// parseOCS decodes an OCS response body.
func parseOCS(body io.Reader) (ocsResponse, error) {
	var resp ocsResponse
	if err := decodeXML(body, &resp); err != nil {
		return resp, err
	}
	return resp, nil
}

// sha256Hex returns the hex SHA-256 of b.
func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// countingReader counts the bytes read through it.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// ctxRenderStream wraps a renderer stream so the SPI contract "Next() must
// unblock on context cancellation and surface it via Err()" holds no matter
// what the renderer does.
type ctxRenderStream struct {
	ctx context.Context
	spi.RenderStream
}

func (s *ctxRenderStream) Next() bool {
	if err := s.ctx.Err(); err != nil {
		return false
	}
	return s.RenderStream.Next()
}

func (s *ctxRenderStream) Err() error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	return s.RenderStream.Err()
}
