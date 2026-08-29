package nextcloud

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// mockNextcloud is a minimal in-memory Nextcloud for tests: a WebDAV file
// store plus an OCS v2 share API. It enforces the same request shapes the
// real server does (auth header, OCS-APIRequest header, MKCOL 405 on
// existing collections, PUT requiring an existing parent) so adapter bugs
// surface as failed assertions rather than silent test passes.
type mockNextcloud struct {
	t *testing.T

	mu       sync.Mutex
	files    map[string][]byte // webdav key ("/evdr/...") → content
	dirs     map[string]bool   // webdav collection keys
	shares   map[string]mockShare
	shareSeq int

	user string // expected service account

	// failStatus returns a canned status for paths with the given prefix,
	// used to simulate backend failures.
	failStatus map[string]int

	// observation hooks
	requests []mockRequest // every request, for assertions
}

type mockShare struct {
	ID          string
	Path        string
	ShareType   string
	ShareWith   string
	Permissions string
	ExpireDate  string
	Name        string
}

type mockRequest struct {
	Method string
	URL    string
	Header http.Header
	Form   url.Values
	Body   []byte
}

// newMockNextcloud starts an in-memory Nextcloud test server.
func newMockNextcloud(t *testing.T, user string) (*mockNextcloud, *httptest.Server) {
	t.Helper()
	m := &mockNextcloud{
		t:      t,
		files:  map[string][]byte{},
		dirs:   map[string]bool{},
		shares: map[string]mockShare{},
		user:   user,
	}
	srv := httptest.NewServer(m)
	return m, srv
}

// basicHeader is the expected Authorization header value.
func (m *mockNextcloud) basicHeader(password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(m.user+":"+password))
}

func (m *mockNextcloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec := mockRequest{Method: r.Method, URL: r.URL.Path, Header: r.Header.Clone()}
	if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/ocs/") {
		// Parse the form before the handler consumes the body.
		_ = r.ParseForm()
		rec.Form = r.PostForm
	}
	// Note: the body is NOT consumed here — handlers read it (and record it
	// into the request log themselves) so WebDAV PUT bodies survive intact.
	m.requests = append(m.requests, rec)

	for prefix, status := range m.failStatus {
		if strings.HasPrefix(r.URL.Path, prefix) {
			w.WriteHeader(status)
			return
		}
	}

	switch {
	case strings.HasPrefix(r.URL.Path, "/ocs/"):
		m.handleOCS(w, r)
	case strings.HasPrefix(r.URL.Path, "/remote.php/dav/files/"):
		m.handleDAV(w, r)
	default:
		http.NotFound(w, r)
	}
}

// recordBody attaches a raw body to the most recently recorded request.
func (m *mockNextcloud) recordBody(body []byte) {
	if len(m.requests) == 0 {
		return
	}
	m.requests[len(m.requests)-1].Body = body
}

// davKey maps a WebDAV request path to the store key.
func (m *mockNextcloud) davKey(path string) (string, bool) {
	prefix := "/remote.php/dav/files/" + m.user + "/evdr"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	key := strings.TrimPrefix(path, prefix)
	if key == "" {
		key = "/"
	}
	return key, true
}

func (m *mockNextcloud) handleDAV(w http.ResponseWriter, r *http.Request) {
	key, ok := m.davKey(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case "MKCOL":
		if m.files[key] != nil || m.dirs[key] {
			w.WriteHeader(http.StatusMethodNotAllowed) // 405: already exists
			return
		}
		m.dirs[key] = true
		w.WriteHeader(http.StatusCreated)
	case http.MethodPut:
		if !m.parentExists(key) {
			w.WriteHeader(http.StatusConflict) // 409: parent missing
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		m.recordBody(body)
		m.files[key] = body
		m.dirs[key] = false
		w.WriteHeader(http.StatusCreated)
	case http.MethodGet, http.MethodHead:
		content, ok := m.files[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	case "COPY":
		src, ok := m.files[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		dest := r.Header.Get("Destination")
		u, err := url.Parse(dest)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		destKey, ok := m.davKey(u.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if m.files[destKey] != nil || m.dirs[destKey] {
			w.WriteHeader(http.StatusPreconditionFailed) // 412: exists
			return
		}
		m.files[destKey] = append([]byte(nil), src...)
		m.dirs[destKey] = false
		w.WriteHeader(http.StatusCreated)
	case "DELETE":
		if m.files[key] != nil || m.dirs[key] {
			delete(m.files, key)
			delete(m.dirs, key)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// parentExists reports whether key's parent collection exists. The adapter
// namespace root and its immediate children count as existing (they are
// implied by the service account's own storage).
func (m *mockNextcloud) parentExists(key string) bool {
	i := strings.LastIndex(key, "/")
	if i <= 0 {
		return true // "/evdr" root
	}
	parent := key[:i]
	if parent == "/evdr" || parent == "" {
		return true
	}
	return m.dirs[parent]
}

func (m *mockNextcloud) handleOCS(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") == "" || r.Header.Get("OCS-APIRequest") != "true" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	const base = "/ocs/v2.php/apps/files_sharing/api/v1/shares"
	switch {
	case r.Method == http.MethodPost && r.URL.Path == base:
		path := r.PostForm.Get("path")
		// The adapter sends the share path relative to the user root
		// ("/evdr/<slug>"); the store keys are relative to the evdr root.
		key := strings.TrimPrefix(path, "/evdr")
		if !m.dirs[key] && m.files[key] == nil {
			writeOCS(w, 404, "Path not found", "")
			return
		}
		m.shareSeq++
		id := fmt.Sprintf("%d", m.shareSeq)
		m.shares[id] = mockShare{
			ID:          id,
			Path:        path,
			ShareType:   r.PostForm.Get("shareType"),
			ShareWith:   r.PostForm.Get("shareWith"),
			Permissions: r.PostForm.Get("permissions"),
			ExpireDate:  r.PostForm.Get("expireDate"),
			Name:        r.PostForm.Get("name"),
		}
		writeOCS(w, 100, "", fmt.Sprintf("<id>%s</id><token>tok%s</token>", id, id))
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, base+"/"):
		id := strings.TrimPrefix(r.URL.Path, base+"/")
		if _, ok := m.shares[id]; !ok {
			writeOCS(w, 404, "Share not found", "")
			return
		}
		delete(m.shares, id)
		writeOCS(w, 100, "", "")
	default:
		http.NotFound(w, r)
	}
}

// writeOCS writes a Nextcloud OCS XML envelope.
func writeOCS(w http.ResponseWriter, code int, message, dataXML string) {
	status := "ok"
	if code != 100 {
		status = "failure"
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<?xml version="1.0"?>
<ocs>
 <meta>
  <status>%s</status>
  <statuscode>%d</statuscode>
  <message>%s</message>
 </meta>
 <data>%s</data>
</ocs>`, status, code, message, dataXML)
}

// hasRequest reports whether any request matched the predicate.
func (m *mockNextcloud) hasRequest(pred func(r mockRequest) bool) bool {
	for _, r := range m.requests {
		if pred(r) {
			return true
		}
	}
	return false
}
