package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Headers the PHP shim sets when proxying an authenticated admin request.
const (
	HeaderUser      = "X-Wpcalc-User"
	HeaderRoles     = "X-Wpcalc-Roles"
	HeaderTimestamp = "X-Wpcalc-Timestamp"
	HeaderSignature = "X-Wpcalc-Signature"
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
}

// NewWordPress builds the sidecar authenticator.
func NewWordPress(secret string) (*WordPress, error) {
	if len(strings.TrimSpace(secret)) < 16 {
		return nil, errors.New("auth: WordPress shared secret must be at least 16 characters")
	}
	return &WordPress{secret: []byte(secret), now: time.Now}, nil
}

// ErrHeadersOverTCP is returned when signed identity headers arrive on a TCP
// listener. It is distinct from ErrUnauthenticated because it means something
// is misconfigured rather than merely unauthenticated, and it deserves a log
// line rather than a login redirect.
var ErrHeadersOverTCP = errors.New("auth: refusing signed identity headers over a TCP listener")

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
	if !hmac.Equal(want, a.mac(user, roles, ts)) {
		return Identity{}, ErrUnauthenticated
	}

	// The plugin only proxies a request here once PHP's current_user_can(
	// 'manage_options') has already passed, regardless of the caller's
	// WordPress role name — a custom role granted that capability is just as
	// trusted as "administrator". So every identity that reaches this point
	// has full access; there is no lesser tier under WordPress mode, and
	// nothing here needs the caller's actual WordPress role list.
	return Identity{Username: user, FullAccess: true}, nil
}

// Sign produces the signature the PHP shim must send. Exported so the e2e test
// and the plugin's own test vector agree with the server byte for byte.
func (a *WordPress) Sign(user, roles string, at time.Time) (timestamp, signature string) {
	ts := strconv.FormatInt(at.Unix(), 10)
	return ts, hex.EncodeToString(a.mac(user, roles, ts))
}

// mac binds all three fields into one message. The separator cannot occur in a
// username or role, so no two distinct field sets can produce the same
// message — without it, user "a" + roles "b,c" and user "a,b" + roles "c"
// would sign identically.
func (a *WordPress) mac(user, roles, ts string) []byte {
	m := hmac.New(sha256.New, a.secret)
	fmt.Fprintf(m, "%s\n%s\n%s", user, roles, ts)
	return m.Sum(nil)
}
