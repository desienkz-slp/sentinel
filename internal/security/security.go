// Package security contains deterministic HTTP boundary checks.
package security

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Role string

const (
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
)

type Operator struct {
	ID   string
	Role Role
}

type Guard struct {
	operatorToken string
	webhookToken  string
	operators     map[string]Operator
	seenMu        *sync.Mutex
	seen          map[string]time.Time
	skew          time.Duration
}

func New(_ string, operatorToken, webhookToken string) Guard {
	g := NewWithOperators("", nil, webhookToken)
	g.operatorToken = strings.TrimSpace(operatorToken)
	return g
}
func NewWithOperators(_ string, operators map[string]Operator, webhookToken string) Guard {
	return Guard{operators: operators, webhookToken: strings.TrimSpace(webhookToken), seenMu: &sync.Mutex{}, seen: make(map[string]time.Time), skew: 5 * time.Minute}
}
func (g Guard) Operator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		op, ok := g.operator(r)
		if !ok {
			if len(g.operators) == 0 && g.operatorToken == "" {
				http.Error(w, "operator authentication must be configured before non-local exposure", 503)
			} else {
				http.Error(w, "operator authentication required", 401)
			}
			return
		}
		withIdentity(next, op).ServeHTTP(w, r)
	})
}
func (g Guard) operator(r *http.Request) (Operator, bool) {
	if g.operatorToken != "" && strings.HasPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ") {
		got := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer "))
		if secureEqual(got, g.operatorToken) {
			return Operator{ID: "operator", Role: RoleAdmin}, true
		}
	}
	id := strings.TrimSpace(r.Header.Get("X-Operator-ID"))
	role := Role(strings.ToLower(strings.TrimSpace(r.Header.Get("X-Operator-Role"))))
	if op, ok := g.operators[id]; ok && op.Role == role && id != "" {
		return op, true
	}
	return Operator{}, false
}
func (g Guard) BrowserMutation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin != "" && origin != r.Header.Get("X-NOC-Allowed-Origin") {
				http.Error(w, "origin rejected", 403)
				return
			}
			if !sameOrigin(r) {
				http.Error(w, "origin required", 403)
				return
			}
			if r.Header.Get("X-CSRF-Token") == "" {
				http.Error(w, "csrf required", 403)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return false
	}
	return strings.Contains(o, r.Host)
}
func (g Guard) Webhook(next http.Handler) http.Handler { return g.SignedWebhook(next) }
func (g Guard) SignedWebhook(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g.webhookToken == "" {
			http.Error(w, "webhook token must be configured before non-local exposure", 503)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "request body too large", 413)
			return
		}
		ts, e := strconv.ParseInt(r.Header.Get("X-NOC-Webhook-Timestamp"), 10, 64)
		if e != nil || time.Since(time.Unix(ts, 0)) > g.skew || time.Since(time.Unix(ts, 0)) < -g.skew {
			http.Error(w, "webhook authentication required", 401)
			return
		}
		sig := r.Header.Get("X-NOC-Webhook-Signature")
		want := Signature(g.webhookToken, ts, body)
		if !secureEqual(sig, want) {
			http.Error(w, "webhook authentication required", 401)
			return
		}
		key := fmt.Sprintf("%d:%s", ts, sig)
		g.seenMu.Lock()
		_, dup := g.seen[key]
		if !dup {
			g.seen[key] = time.Now()
		}
		for k, t := range g.seen {
			if time.Since(t) > g.skew {
				delete(g.seen, k)
			}
		}
		g.seenMu.Unlock()
		if dup {
			http.Error(w, "replayed webhook", 401)
			return
		}
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		next.ServeHTTP(w, r)
	})
}
func Signature(secret string, timestamp int64, body []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(h, "%d.", timestamp)
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}
func secureEqual(a, b string) bool {
	if a == "" || b == "" || len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func withIdentity(next http.Handler, op Operator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := contextWith(r, op)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type contextKey int

const operatorKey contextKey = 1

func contextWith(r *http.Request, op Operator) context.Context {
	return context.WithValue(r.Context(), operatorKey, op)
}
func Identity(r *http.Request) (Operator, bool) {
	op, ok := r.Context().Value(operatorKey).(Operator)
	return op, ok
}
