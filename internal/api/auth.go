package api

import (
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

	failMu    sync.Mutex
	failIPs   map[string][]time.Time
}

func NewAuth(st *store.Store, secret string) *Auth {
	return &Auth{St: st, Secret: []byte(secret), Expire: 24 * time.Hour, failIPs: map[string][]time.Time{}}
}

func (a *Auth) Issue(username string, uid int64) (string, error) {
	claims := jwt.MapClaims{
		"sub":  username,
		"uid":  uid,
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
	a.failIPs[ip] = recent
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
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
