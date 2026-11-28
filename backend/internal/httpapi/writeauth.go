package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
)

// Headers of the two optional write-side credentials.
const (
	// WriteTokenHeader carries the world's write token on every mutation.
	WriteTokenHeader = "X-Parallel-Write-Token"
	// LLMCodeHeader carries the optional LLM access code.
	LLMCodeHeader = "X-Parallel-LLM-Code"

	// maxCredentialLen bounds a credential header before hashing.
	maxCredentialLen = 512

	DefaultWriteTokenFailuresPerMinute = 20
	DefaultLLMDuelsPerDayPerIP         = 5
	MaxLLMDuelsPerDayPerIP             = 1000
)

// newWriteToken returns a random 256-bit token (base64url, 43 characters) and
// the SHA-256 hash that is stored in its place.
func newWriteToken() (token string, hash []byte, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(b[:])
	h := sha256.Sum256([]byte(token))
	return token, h[:], nil
}

// windowCounter counts events per key in fixed windows.
type windowCounter struct {
	mu    sync.Mutex
	span  time.Duration
	now   func() time.Time
	win   map[string]*hourWindow
	sweep time.Time
}

func newWindowCounter(span time.Duration, now func() time.Time) *windowCounter {
	return &windowCounter{span: span, now: now, win: map[string]*hourWindow{}}
}

func (c *windowCounter) current(key string, now time.Time) *hourWindow {
	if now.Sub(c.sweep) > c.span {
		for k, w := range c.win {
			if now.Sub(w.start) >= c.span {
				delete(c.win, k)
			}
		}
		c.sweep = now
	}
	w := c.win[key]
	if w == nil || now.Sub(w.start) >= c.span {
		w = &hourWindow{start: now}
		c.win[key] = w
	}
	return w
}

// blocked reports whether key has reached max events, and the seconds left.
func (c *windowCounter) blocked(key string, max int) (bool, int) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.current(key, now)
	if w.n >= max {
		return true, int((c.span - now.Sub(w.start)).Seconds()) + 1
	}
	return false, 0
}

func (c *windowCounter) add(key string) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current(key, now).n++
}

// take counts one event unless key is already at max.
func (c *windowCounter) take(key string, max int) (bool, int) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.current(key, now)
	if w.n >= max {
		return false, int((c.span - now.Sub(w.start)).Seconds()) + 1
	}
	w.n++
	return true, 0
}

// hashEqual compares two SHA-256 digests in constant time.
func hashEqual(a, b []byte) bool { return subtle.ConstantTimeCompare(a, b) == 1 }

// secretEqual compares two secrets in constant time (over their digests, so
// the length does not leak).
func secretEqual(got, want string) bool {
	a, b := sha256.Sum256([]byte(got)), sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// requireWrite guards a mutating route whose path carries a branch id
// (kind "branch") or a duel id (kind "duel"). It runs before any handler work,
// so a refused request changes nothing and takes no simulation slot.
//
// A world created with a write token needs a matching X-Parallel-Write-Token.
// A legacy world (no stored hash) is read-only in production and writable
// otherwise. Failed attempts are counted per client IP; past the limit the
// caller gets 429 until the minute ends. The token is never logged.
func (s *Server) requireWrite(kind string) func(http.Handler) http.Handler {
	param := "branchId"
	if kind == "duel" {
		param = "duelId"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := s.clientIP(r)
			if blocked, retry := s.tokenFails.blocked(ip, s.tokenFailMax); blocked {
				w.Header().Set("Retry-After", strconv.Itoa(retry))
				writeProblem(w, r, newProblem("rate-limited", "too many invalid write tokens from this address; retry in a minute"))
				return
			}
			id := chi.URLParam(r, param)
			if !isUUID(id) {
				s.fail(w, r, newProblem("not-found", "the world, branch or duel does not exist"))
				return
			}
			var wid app.WorldID
			if kind == "duel" {
				du, err := s.duels.GetDuel(r.Context(), app.DuelID(id))
				if err != nil {
					s.fail(w, r, err)
					return
				}
				wid = du.WorldID
			} else {
				b, err := s.svc.GetBranch(r.Context(), app.BranchID(id))
				if err != nil {
					s.fail(w, r, err)
					return
				}
				wid = b.WorldID
			}
			world, _, err := s.svc.GetWorld(r.Context(), wid)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			if len(world.WriteTokenHash) == 0 {
				if s.production {
					s.tokenFails.add(ip)
					writeProblem(w, r, newProblem("write-token-invalid",
						"this world predates write tokens and is read-only"))
					return
				}
				next.ServeHTTP(w, r) // development data keeps working locally
				return
			}
			tok := r.Header.Get(WriteTokenHeader)
			if tok == "" {
				writeProblem(w, r, newProblem("write-token-required",
					"changing this world needs its write token in the "+WriteTokenHeader+" header; a shared link is read-only"))
				return
			}
			got := sha256.Sum256([]byte(tok))
			if len(tok) > maxCredentialLen || !hashEqual(got[:], world.WriteTokenHash) {
				s.tokenFails.add(ip)
				writeProblem(w, r, newProblem("write-token-invalid", "the write token does not match this world"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// guardLLMCreate applies to every request that starts an LLM-backed manager:
// the optional access code, then the per-IP daily cap. Nothing is checked when
// the LLM is not configured (the request then plays the rule-based fallback
// and makes no provider call). It returns false after writing the problem.
func (s *Server) guardLLMCreate(w http.ResponseWriter, r *http.Request) bool {
	if ok, _ := s.svc.LLMAvailable(); !ok {
		return true
	}
	if s.llmCode != "" {
		ip := s.clientIP(r)
		if blocked, retry := s.tokenFails.blocked(ip, s.tokenFailMax); blocked {
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			writeProblem(w, r, newProblem("rate-limited", "too many invalid credentials from this address; retry in a minute"))
			return false
		}
		got := r.Header.Get(LLMCodeHeader)
		if len(got) > maxCredentialLen || !secretEqual(got, s.llmCode) {
			s.tokenFails.add(ip)
			writeProblem(w, r, newProblem("llm-access-code-required",
				"the LLM opponent needs the access code in the "+LLMCodeHeader+" header; the rule-based AI needs none"))
			return false
		}
	}
	if ok, retry := s.llmDaily.take(s.clientIP(r), s.llmPerDay); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		writeProblem(w, r, newProblem("rate-limited", "too many LLM opponents started from this address today"))
		return false
	}
	return true
}
