package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rolsim/wpcalc/internal/domain"
)

// Headers the PHP shim sets when proxying an authenticated request.
const (
	HeaderUser      = "X-Wpcalc-User"
	HeaderRoles     = "X-Wpcalc-Roles"
	HeaderTimestamp = "X-Wpcalc-Timestamp"
	HeaderSignature = "X-Wpcalc-Signature"

	// HeaderScope distinguishes the wp-admin proxy (ScopeAdmin) from the
	// frontend shortcode proxy (ScopeSelf) — see the Scope constants below.
	HeaderScope = "X-Wpcalc-Scope"
)

// Scope values carried in HeaderScope and bound into the signature.
const (
	// ScopeAdmin is the existing wp-admin proxy: PHP has already gated the
	// request on manage_options, so the resulting identity is FullAccess.
	// Also the default when the header is absent, so a caller that has no
	// reason to think about scope (nothing outside this package sends the
	// frontend proxy's headers) keeps the original behavior.
	ScopeAdmin = "admin"

	// ScopeSelf is the frontend shortcode proxy: any logged-in WordPress
	// user, not necessarily one who can manage_options. The resulting
	// identity is never FullAccess — it is exactly what a linked wpcalc
	// account's own UserRoles grant, resolved via ScopedUserStore.
	ScopeSelf = "self"
)

// SignatureSkew bounds how stale a signed header set may be. It exists to stop
// a captured header set being replayed indefinitely; the window is generous
// enough to survive clock drift between PHP-FPM and the sidecar.
const SignatureSkew = 5 * time.Minute

// WordPress trusts identity asserted by the PHP shim, under two conditions
// that must both hold.
//
// First, the request must have arrived over the unix socket. Headers are
// forgeable by anything that can reach the listener, so over TCP this adapter
// refuses regardless of how good the signature is — otherwise exposing the
// port for a moment would be enough for anyone to assert they are an admin.
//
// Second, the headers must carry a fresh HMAC over their own contents using a
// secret shared with the plugin. The socket check alone would trust any local
// process that can open the socket file.
type WordPress struct {
	secret []byte
	now    func() time.Time
	store  ScopedUserStore
}

// ScopedUserStore is the slice of persistence a ScopeSelf request needs to
// resolve a WordPress username to a wpcalc account's own UserRoles.
//
// Declared here rather than imported from store, same reasoning as
// accounts.go's UserStore: this package stays testable without a database,
// and the dependency points one way.
type ScopedUserStore interface {
	UserByUsername(ctx context.Context, username string) (domain.User, error)
	UserRolesForUser(ctx context.Context, userID int64) ([]domain.UserRole, error)
	RolePermissionsFor(ctx context.Context, roleIDs []string) (map[string][]string, error)
}

// NewWordPress builds the sidecar authenticator.
func NewWordPress(secret string) (*WordPress, error) {
	if len(strings.TrimSpace(secret)) < 16 {
		return nil, errors.New("auth: WordPress shared secret must be at least 16 characters")
	}
	return &WordPress{secret: []byte(secret), now: time.Now}, nil
}

// WithStore enables ScopeSelf requests by giving the authenticator somewhere
// to resolve a WordPress username to a wpcalc account. Without it, every
// ScopeSelf request fails with ErrNoLinkedAccount — the admin (ScopeAdmin)
// path never needs a store and works identically either way.
func (a *WordPress) WithStore(store ScopedUserStore) *WordPress {
	a.store = store
	return a
}

// ErrHeadersOverTCP is returned when signed identity headers arrive on a TCP
// listener. It is distinct from ErrUnauthenticated because it means something
// is misconfigured rather than merely unauthenticated, and it deserves a log
// line rather than a login redirect.
var ErrHeadersOverTCP = errors.New("auth: refusing signed identity headers over a TCP listener")

// ErrNoLinkedAccount is returned for a ScopeSelf request when the WordPress
// username has no matching wpcalc account, or that account holds no roles.
// It is distinct from ErrUnauthenticated so callers can fall back to a
// second identity source (a local wpcalc login) instead of just refusing —
// see the composite authenticator built in cmd/wpcalc/serve.go.
var ErrNoLinkedAccount = errors.New("auth: no wpcalc account is linked to this WordPress user")

// Identify validates the shim's assertion.
func (a *WordPress) Identify(r *http.Request) (Identity, error) {
	user := r.Header.Get(HeaderUser)
	sig := r.Header.Get(HeaderSignature)

	if ConnKindFrom(r.Context()) != ConnUnix {
		// Fail loudly rather than silently: if headers are present, someone is
		// actively trying, and if they are absent this still cannot succeed.
		if user != "" || sig != "" {
			return Identity{}, ErrHeadersOverTCP
		}
		return Identity{}, ErrUnauthenticated
	}

	if user == "" || sig == "" {
		return Identity{}, ErrUnauthenticated
	}

	roles := r.Header.Get(HeaderRoles)
	ts := r.Header.Get(HeaderTimestamp)
	scope := r.Header.Get(HeaderScope)
	if scope == "" {
		scope = ScopeAdmin
	}

	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return Identity{}, ErrUnauthenticated
	}
	age := a.now().Sub(time.Unix(unix, 0))
	if age > SignatureSkew || age < -SignatureSkew {
		return Identity{}, ErrUnauthenticated
	}

	want, err := hex.DecodeString(sig)
	if err != nil {
		return Identity{}, ErrUnauthenticated
	}
	if !hmac.Equal(want, a.mac(user, roles, ts, scope)) {
		return Identity{}, ErrUnauthenticated
	}

	if scope == ScopeSelf {
		return a.scopedIdentity(r.Context(), user)
	}

	// The plugin only proxies a ScopeAdmin request here once PHP's
	// current_user_can('manage_options') has already passed, regardless of
	// the caller's WordPress role name — a custom role granted that
	// capability is just as trusted as "administrator". So every ScopeAdmin
	// identity has full access; there is no lesser tier there, and nothing
	// here needs the caller's actual WordPress role list.
	return Identity{Username: user, FullAccess: true}, nil
}

// scopedIdentity resolves a ScopeSelf request to the wpcalc account whose
// username matches the WordPress username, mirroring Accounts.identityFor
// (accounts.go) — but keyed by username rather than a resolved domain.User,
// since this path has no session cookie to start from. Unlike the admin
// path, this identity is never FullAccess: it is exactly what the linked
// account's own UserRoles grant, so an ordinary employee sees only what
// buildGridView already lets any non-FullAccess identity see.
func (a *WordPress) scopedIdentity(ctx context.Context, username string) (Identity, error) {
	if a.store == nil {
		return Identity{}, ErrNoLinkedAccount
	}
	u, err := a.store.UserByUsername(ctx, username)
	if err != nil {
		return Identity{}, ErrNoLinkedAccount
	}
	userRoles, err := a.store.UserRolesForUser(ctx, u.ID)
	if err != nil {
		return Identity{}, err
	}
	// No roles is exactly as useless as no account: nothing would be visible
	// under this identity, so it degrades the same way as ErrNoLinkedAccount
	// rather than as a technically-successful but empty identity.
	if len(userRoles) == 0 {
		return Identity{}, ErrNoLinkedAccount
	}

	roleIDs := make([]string, 0, len(userRoles))
	seen := make(map[string]bool, len(userRoles))
	for _, ur := range userRoles {
		if !seen[ur.RoleID] {
			seen[ur.RoleID] = true
			roleIDs = append(roleIDs, ur.RoleID)
		}
	}
	perms, err := a.store.RolePermissionsFor(ctx, roleIDs)
	if err != nil {
		return Identity{}, err
	}

	return Identity{
		UserID:          u.ID,
		Username:        u.Username,
		Language:        u.Language,
		UserRoles:       userRoles,
		RolePermissions: perms,
	}, nil
}

// Sign produces the signature the PHP shim must send. Exported so the e2e test
// and the plugin's own test vector agree with the server byte for byte.
func (a *WordPress) Sign(user, roles, scope string, at time.Time) (timestamp, signature string) {
	ts := strconv.FormatInt(at.Unix(), 10)
	return ts, hex.EncodeToString(a.mac(user, roles, ts, scope))
}

// mac binds all four fields into one message. The separator cannot occur in
// a username or role, so no two distinct field sets can produce the same
// message — without it, user "a" + roles "b,c" and user "a,b" + roles "c"
// would sign identically. scope is bound in too, so ScopeAdmin cannot be
// downgraded to (or ScopeSelf upgraded from) the other by stripping/editing
// an unsigned header.
func (a *WordPress) mac(user, roles, ts, scope string) []byte {
	m := hmac.New(sha256.New, a.secret)
	fmt.Fprintf(m, "%s\n%s\n%s\n%s", user, roles, ts, scope)
	return m.Sum(nil)
}
