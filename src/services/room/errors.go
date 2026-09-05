package room

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ivenkwan/evdr/src/spi"
)

// httpError is an HTTP-level failure with a stable machine-readable code.
// Codes are stable API surface — the portal/admin BFFs surface them to the
// user.
type httpError struct {
	Status int
	Code   string
	Err    error // underlying cause, for logging; never sent verbatim
}

func (e *httpError) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return fmt.Sprintf("%s: %v", e.Code, e.Err)
}

func (e *httpError) Unwrap() error { return e.Err }

func httpErr(status int, code string, err error) *httpError {
	return &httpError{Status: status, Code: code, Err: err}
}

func badRequest(msg string) *httpError {
	return &httpError{Status: http.StatusBadRequest, Code: "bad_request", Err: errors.New(msg)}
}

func internalErr(err error) *httpError {
	return &httpError{Status: http.StatusInternalServerError, Code: "internal", Err: err}
}

// Error codes — stable API surface.
const (
	codeUnauthenticated    = "unauthenticated"
	codeAccessDenied       = "access_denied"
	codeBadRequest         = "bad_request"
	codePayloadTooLarge    = "payload_too_large"
	codeRoomNotFound       = "room_not_found"
	codeRoomExists         = "room_exists"
	codeRoomSealed         = "room_sealed"
	codeRoomClosed         = "room_closed"
	codeDocumentNotFound   = "document_not_found"
	codeGrantNotFound      = "grant_not_found"
	codeInvalidGrant       = "invalid_grant"
	codeRetentionViolation = "retention_violation"
	codeQuotaExceeded      = "quota_exceeded"
	codeUnsupported        = "unsupported"
	codeInternal           = "internal"
)

// mapSPIError translates an SPI failure into its HTTP form. This is the only
// place SPI sentinels become HTTP codes, so the mapping stays consistent for
// every endpoint. Wrong-tenant requests surface here as the SPI's own
// errors: room/document-scoped mismatches become the not-found codes and
// create mismatches become access_denied — mirroring the SPI README's
// no-existence-oracle semantics.
func mapSPIError(err error) *httpError {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, spi.ErrRoomNotFound):
		return httpErr(http.StatusNotFound, codeRoomNotFound, err)
	case errors.Is(err, spi.ErrDocumentNotFound):
		return httpErr(http.StatusNotFound, codeDocumentNotFound, err)
	case errors.Is(err, spi.ErrGrantNotFound):
		return httpErr(http.StatusNotFound, codeGrantNotFound, err)
	case errors.Is(err, spi.ErrRoomExists):
		return httpErr(http.StatusConflict, codeRoomExists, err)
	case errors.Is(err, spi.ErrRoomSealed):
		return httpErr(http.StatusConflict, codeRoomSealed, err)
	case errors.Is(err, spi.ErrRoomClosed):
		return httpErr(http.StatusConflict, codeRoomClosed, err)
	case errors.Is(err, spi.ErrInvalidGrant):
		return httpErr(http.StatusBadRequest, codeInvalidGrant, err)
	case errors.Is(err, spi.ErrRetentionViolation):
		return httpErr(http.StatusConflict, codeRetentionViolation, err)
	case errors.Is(err, spi.ErrAccessDenied):
		return httpErr(http.StatusForbidden, codeAccessDenied, err)
	case errors.Is(err, spi.ErrQuotaExceeded):
		return httpErr(http.StatusInsufficientStorage, codeQuotaExceeded, err)
	case errors.Is(err, spi.ErrUnsupported):
		return httpErr(http.StatusNotImplemented, codeUnsupported, err)
	case errors.Is(err, context.DeadlineExceeded):
		return httpErr(http.StatusGatewayTimeout, "upstream_timeout", err)
	default:
		return internalErr(err)
	}
}
