package room

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/ivenkwan/evdr/src/spi"
)

// ErrUnauthenticated is returned by Authenticator implementations when the
// request carries no valid credential. Mapped to HTTP 401 by the server.
var ErrUnauthenticated = errors.New("room: unauthenticated")

// Principal is the authenticated caller of one request. Tenant identity is
// established here, from verified credentials — never from client-supplied
// parameters (SR-2.2, AGENTS.md §6). Production: resolved from validated
// Keycloak OIDC/JWT claims (TR-6.x, a later wave).
type Principal struct {
	// TenantID is the tenant the principal acts in.
	TenantID spi.TenantID
	// Actor is the SPI actor the principal maps to.
	Actor spi.Actor
}

// Authenticator resolves the authenticated Principal from an HTTP request.
// It is the seam where production identity validation (Keycloak OIDC/JWT,
// TR-6.x) replaces the dev implementation. Implementations must not log
// credentials and must fail closed: any malformed or missing credential
// returns ErrUnauthenticated (or a wrapped sentinel matching errors.Is).
type Authenticator interface {
	Authenticate(r *http.Request) (Principal, error)
}

// DevAuthenticator is the development identity implementation (TR-6.x is a
// later wave). It validates a shared secret carried as
// "Authorization: Bearer <secret>" against the configured Secret using a
// constant-time compare, and maps every authenticated request to one fixed
// principal (user + tenant taken from configuration).
//
// Explicitly NOT production-grade: there is one principal for the whole
// deployment, and the secret is a single shared token. It exists so the
// portal/admin BFFs (which hold the secret server-side) can exercise the
// full vertical slice. The service NEVER accepts a user or tenant from the
// client — any such headers are ignored. Production replaces this with
// OIDC/JWT validation behind the same Authenticator interface.
type DevAuthenticator struct {
	secret   string
	tenantID spi.TenantID
	actor    spi.Actor
}

// DevPrincipalConfig configures NewDevAuthenticator.
type DevPrincipalConfig struct {
	// Secret is the shared development secret. Required; never logged.
	Secret string
	// TenantID is the tenant every authenticated principal acts in.
	TenantID spi.TenantID
	// UserID is the principal ID recorded as the actor on SPI calls
	// (room.CreatedBy, version.CreatedBy, ...). Defaults to
	// "admin@evdr.local".
	UserID string
	// DisplayName is the human-readable actor name. Defaults to
	// "EVDR Admin".
	DisplayName string
	// Kind defaults to spi.ActorInternalUser.
	Kind spi.ActorKind
}

// NewDevAuthenticator returns an Authenticator for the development secret
// scheme described on DevAuthenticator. It fails (error) when Secret or
// TenantID is empty — a service with an empty secret or unknown tenant would
// be a security hazard, not a convenience.
func NewDevAuthenticator(cfg DevPrincipalConfig) (*DevAuthenticator, error) {
	if cfg.Secret == "" {
		return nil, errors.New("room: dev authenticator requires a non-empty secret (EVDR_ROOM_SERVICE_DEV_SECRET)")
	}
	if cfg.TenantID == "" {
		return nil, errors.New("room: dev authenticator requires a tenant (EVDR_ROOM_SERVICE_DEV_TENANT)")
	}
	if cfg.UserID == "" {
		cfg.UserID = "admin@evdr.local"
	}
	if cfg.DisplayName == "" {
		cfg.DisplayName = "EVDR Admin"
	}
	if cfg.Kind == "" {
		cfg.Kind = spi.ActorInternalUser
	}
	return &DevAuthenticator{
		secret:   cfg.Secret,
		tenantID: cfg.TenantID,
		actor: spi.Actor{
			Kind:        cfg.Kind,
			ID:          cfg.UserID,
			DisplayName: cfg.DisplayName,
		},
	}, nil
}

// Authenticate validates the bearer secret and returns the fixed principal.
// The secret is compared in constant time; a mismatch or malformed header
// returns ErrUnauthenticated. Client-supplied identity headers are ignored
// by construction — this method reads only the credential.
func (a *DevAuthenticator) Authenticate(r *http.Request) (Principal, error) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return Principal{}, ErrUnauthenticated
	}
	token := strings.TrimSpace(h[len(prefix):])
	if token == "" {
		return Principal{}, ErrUnauthenticated
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(a.secret)) != 1 {
		return Principal{}, ErrUnauthenticated
	}
	return Principal{TenantID: a.tenantID, Actor: a.actor}, nil
}
