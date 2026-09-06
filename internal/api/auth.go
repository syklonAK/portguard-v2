package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"portguard/internal/store"
)

type Auth struct {
	St     *store.Store
	Secret []byte
	Expire time.Duration

	failMu  sync.Mutex
	failIPs map[string][]time.Time
}

func NewAuth(st *store.Store, secret string) *Auth {
	return &Auth{St: st, Secret: []byte(secret), Expire: 24 * time.Hour, failIPs: map[string][]time.Time{}}
}

func (a *Auth) Issue(username string, uid int64) (string, error) {
	return a.IssueRole(username, uid, "owner")
}

// IssueRole signs a session token carrying the caller's RBAC role.
func (a *Auth) IssueRole(username string, uid int64, role string) (string, error) {
	claims := jwt.MapClaims{
		"sub":  username,
		"uid":  uid,
		"role": role,
		"iat":  time.Now().Unix(),
		"exp":  time.Now().Add(a.Expire).Unix(),
		"iss":  "portguard",
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.Secret)
}

func (a *Auth) Parse(tokenStr string) (jwt.MapClaims, error) {
	tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("bad signing method")
		}
		return a.Secret, nil
	})
	if err != nil || !tok.Valid {
		return nil, errors.New("invalid token")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("bad claims")
	}
	return claims, nil
}

// loginGuard: max 5 failed logins per minute per IP.
func (a *Auth) Allow(ip string) bool {
	a.failMu.Lock()
	defer a.failMu.Unlock()
	now := time.Now()
	recent := a.failIPs[ip][:0]
	for _, t := range a.failIPs[ip] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		// prune the entry entirely so the map can't grow without bound
		// under a flood of unique spoofed/unroutable source IPs
		delete(a.failIPs, ip)
	} else {
		a.failIPs[ip] = recent
	}
	return len(recent) < 5
}

func (a *Auth) Fail(ip string) {
	a.failMu.Lock()
	defer a.failMu.Unlock()
	a.failIPs[ip] = append(a.failIPs[ip], time.Now())
}

func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			unauthorized(w)
			return
		}
		claims, err := a.Parse(strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			unauthorized(w)
			return
		}
		ctx := withActor(r.Context(), toString(claims["sub"]))
		// attach the role for RBAC checks downstream
		if role := toString(claims["role"]); role != "" {
			ctx = withRole(ctx, role)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole wraps handlers with a minimum-role check. Viewers can read,
// operators can deploy, admins manage everything operational, owners hold
// security/user management.
func (a *Auth) RequireRole(min string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := roleFrom(r.Context())
		if store.RoleRank(role) < store.RoleRank(min) {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "your role (" + role + ") is not allowed to perform this action (needs " + min + ")",
			})
			return
		}
		next(w, r)
	}
}

// ctxRoleKey carries the caller's RBAC role through the request context.
type ctxRoleKeyType int

const ctxRoleKey ctxRoleKeyType = 1

func withRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, ctxRoleKey, role)
}

func roleFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxRoleKey).(string); ok {
		return v
	}
	return "viewer"
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func RandomSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
