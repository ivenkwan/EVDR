package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ivenkwan/evdr/src/spi"
)

// typedError is an HTTP-layer error with a stable code.
type typedError struct {
	status  int
	code    string
	message string
}

func (e *typedError) Error() string { return e.message }

func errBadRequest(msg string) error {
	return &typedError{status: http.StatusBadRequest, code: "bad_request", message: msg}
}

func errUnauthorized(msg string) error {
	return &typedError{status: http.StatusUnauthorized, code: "unauthorized", message: msg}
}

func errPageNotFound(page int) error {
	return &typedError{status: http.StatusNotFound, code: "page_not_found", message: fmt.Sprintf("page %d not found", page)}
}

// mapError translates service and SPI errors into HTTP statuses. Details are
// generic by design — backend specifics never reach the client, and
// cross-tenant probes surface as plain 404s because the SPI already scopes
// them to not-found (no existence oracle).
func mapError(err error) (status int, code string) {
	var te *typedError
	switch {
	case errors.As(err, &te):
		return te.status, te.code
	case errors.Is(err, spi.ErrRoomNotFound),
		errors.Is(err, spi.ErrDocumentNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, spi.ErrAccessDenied):
		return http.StatusForbidden, "access_denied"
	case errors.Is(err, spi.ErrUnsupported),
		errors.Is(err, spi.ErrQuotaExceeded):
		return http.StatusNotImplemented, "unsupported"
	case errors.Is(err, context.Canceled):
		return 499, "gone" // client went away — nothing is written back
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "timeout"
	default:
		return http.StatusInternalServerError, "internal"
	}
}

// writeError maps an internal error to the HTTP status and envelope.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := mapError(err)
	if code == "gone" {
		return // client disconnected; nothing to write
	}
	if code == "internal" {
		s.logError("viewer.internal_error", identityInfo{}, "", "", err.Error(), err)
	}
	msg := map[string]string{
		"bad_request":    err.Error(),
		"unauthorized":   err.Error(),
		"not_found":      "not found",
		"page_not_found": err.Error(),
		"access_denied":  "access denied",
		"unsupported":    "operation not supported",
		"timeout":        "request timed out",
		"internal":       "internal error",
	}[code]
	resp := errResponse{}
	resp.Error.Code = code
	resp.Error.Message = msg
	writeJSON(w, status, resp)
}
