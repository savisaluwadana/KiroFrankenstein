package llm

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Environment variable names.
const (
	EnvBaseURL        = "LLM_BASE_URL"
	EnvAPIKey         = "LLM_API_KEY"
	EnvModel          = "LLM_MODEL"
	EnvTimeoutSeconds = "LLM_REQUEST_TIMEOUT_SECONDS"
	EnvMaxCalls       = "LLM_MAX_CALLS_PER_WORLD"
	EnvMaxTokens      = "LLM_MAX_TOKENS_PER_CALL"
	EnvMaxTotalTokens = "LLM_MAX_TOTAL_TOKENS_PER_WORLD"
	EnvHTTPReferer    = "LLM_HTTP_REFERER"
	EnvAppTitle       = "LLM_APP_TITLE"

	EnvMaxPromptBytes     = "LLM_MAX_PROMPT_BYTES"
	EnvGlobalMaxCalls     = "LLM_GLOBAL_MAX_CALLS"
	EnvGlobalMaxTokens    = "LLM_GLOBAL_MAX_TOKENS"
	EnvGlobalWindow       = "LLM_GLOBAL_WINDOW"
	EnvMaxConcurrentCalls = "LLM_MAX_CONCURRENT_REQUESTS"
)

// Defaults and bounds of the numeric settings. There is deliberately no
// default model and no default key.
const (
	DefaultTimeoutSeconds = 20
	MinTimeoutSeconds     = 1
	MaxTimeoutSeconds     = 120

	DefaultMaxCallsPerWorld = 40
	MinMaxCallsPerWorld     = 1
	MaxMaxCallsPerWorld     = 200

	DefaultMaxTokensPerCall = 1024
	MinMaxTokensPerCall     = 64
	MaxMaxTokensPerCall     = 8192

	DefaultMaxTotalTokensPerWorld = 200000
	MinMaxTotalTokensPerWorld     = 1000
	MaxMaxTotalTokensPerWorld     = 5000000

	DefaultMaxPromptBytes = 32768
	MinMaxPromptBytes     = 1024
	MaxMaxPromptBytes     = 262144

	// The global limits cover all worlds of the server within one UTC
	// calendar day (the only supported window, "day").
	DefaultGlobalMaxCalls = 2000
	MinGlobalMaxCalls     = 1
	MaxGlobalMaxCalls     = 1000000

	DefaultGlobalMaxTokens = 5000000
	MinGlobalMaxTokens     = 1000
	MaxGlobalMaxTokens     = 1000000000

	DefaultMaxConcurrentRequests = 4
	MinMaxConcurrentRequests     = 1
	MaxMaxConcurrentRequests     = 64

	// GlobalWindowDay is the only value of LLM_GLOBAL_WINDOW.
	GlobalWindowDay = "day"
)

// ErrConfig is wrapped by every configuration error. The messages name the
// variable and never contain the API key.
var ErrConfig = errors.New("llm: invalid configuration")

// Config is the LLM provider configuration. The LLM opponent is available
// only when BaseURL, APIKey and Model are all non-empty (Available).
type Config struct {
	BaseURL string
	APIKey  string
	Model   string

	RequestTimeout         time.Duration
	MaxCallsPerWorld       int
	MaxTokensPerCall       int
	MaxTotalTokensPerWorld int

	// MaxPromptBytes caps system+user prompt bytes; larger prompts are not
	// sent. GlobalMaxCalls and GlobalMaxTokens are the server-wide limits per
	// UTC day. MaxConcurrentRequests bounds in-flight provider requests.
	MaxPromptBytes        int
	GlobalMaxCalls        int
	GlobalMaxTokens       int
	MaxConcurrentRequests int

	// HTTPReferer and AppTitle are optional OpenRouter attribution headers.
	HTTPReferer string
	AppTitle    string
}

// LoadConfig reads the configuration through getenv (os.Getenv in
// production). Unset numeric variables take their defaults; out-of-range or
// malformed values are rejected with an error naming the variable and its
// bounds. A partially set provider triple is not an error: Available reports
// false.
func LoadConfig(getenv func(string) string) (Config, error) {
	get := func(k string) string { return strings.TrimSpace(getenv(k)) }
	c := Config{
		BaseURL:     strings.TrimRight(get(EnvBaseURL), "/"),
		APIKey:      get(EnvAPIKey),
		Model:       get(EnvModel),
		HTTPReferer: get(EnvHTTPReferer),
		AppTitle:    get(EnvAppTitle),
	}
	var err error
	var secs int
	if secs, err = intVar(get, EnvTimeoutSeconds, DefaultTimeoutSeconds, MinTimeoutSeconds, MaxTimeoutSeconds); err != nil {
		return Config{}, err
	}
	c.RequestTimeout = time.Duration(secs) * time.Second
	if c.MaxCallsPerWorld, err = intVar(get, EnvMaxCalls, DefaultMaxCallsPerWorld, MinMaxCallsPerWorld, MaxMaxCallsPerWorld); err != nil {
		return Config{}, err
	}
	if c.MaxTokensPerCall, err = intVar(get, EnvMaxTokens, DefaultMaxTokensPerCall, MinMaxTokensPerCall, MaxMaxTokensPerCall); err != nil {
		return Config{}, err
	}
	if c.MaxTotalTokensPerWorld, err = intVar(get, EnvMaxTotalTokens, DefaultMaxTotalTokensPerWorld, MinMaxTotalTokensPerWorld, MaxMaxTotalTokensPerWorld); err != nil {
		return Config{}, err
	}
	if c.MaxPromptBytes, err = intVar(get, EnvMaxPromptBytes, DefaultMaxPromptBytes, MinMaxPromptBytes, MaxMaxPromptBytes); err != nil {
		return Config{}, err
	}
	if c.GlobalMaxCalls, err = intVar(get, EnvGlobalMaxCalls, DefaultGlobalMaxCalls, MinGlobalMaxCalls, MaxGlobalMaxCalls); err != nil {
		return Config{}, err
	}
	if c.GlobalMaxTokens, err = intVar(get, EnvGlobalMaxTokens, DefaultGlobalMaxTokens, MinGlobalMaxTokens, MaxGlobalMaxTokens); err != nil {
		return Config{}, err
	}
	if c.MaxConcurrentRequests, err = intVar(get, EnvMaxConcurrentCalls, DefaultMaxConcurrentRequests, MinMaxConcurrentRequests, MaxMaxConcurrentRequests); err != nil {
		return Config{}, err
	}
	if w := get(EnvGlobalWindow); w != "" && w != GlobalWindowDay {
		return Config{}, fmt.Errorf("%w: %s must be %q", ErrConfig, EnvGlobalWindow, GlobalWindowDay)
	}
	if c.BaseURL != "" {
		if err := validateBaseURL(c.BaseURL); err != nil {
			return Config{}, err
		}
	}
	return c, nil
}

func intVar(get func(string) string, name string, def, lo, hi int) (int, error) {
	s := get(name)
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%w: %s must be an integer between %d and %d", ErrConfig, name, lo, hi)
	}
	return n, nil
}

// validateBaseURL requires https (http only for localhost, 127.0.0.1 and ::1)
// and rejects credentials, queries and fragments. Error text never echoes the
// URL, which could carry a secret.
func validateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%w: %s must be an absolute http(s) URL", ErrConfig, EnvBaseURL)
	}
	if u.User != nil {
		return fmt.Errorf("%w: %s must not contain credentials", ErrConfig, EnvBaseURL)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%w: %s must not contain a query or fragment", ErrConfig, EnvBaseURL)
	}
	switch u.Scheme {
	case "https":
	case "http":
		h := u.Hostname()
		if h != "localhost" && h != "127.0.0.1" && h != "::1" {
			if ip := net.ParseIP(h); ip == nil || !ip.IsLoopback() {
				return fmt.Errorf("%w: %s must use https (http is allowed only for localhost)", ErrConfig, EnvBaseURL)
			}
		}
	default:
		return fmt.Errorf("%w: %s must use https", ErrConfig, EnvBaseURL)
	}
	return nil
}

// withDefaults fills unset (zero) limits of a Config built without LoadConfig
// (tests); LoadConfig always sets them.
func (c Config) withDefaults() Config {
	if c.MaxPromptBytes <= 0 {
		c.MaxPromptBytes = DefaultMaxPromptBytes
	}
	if c.GlobalMaxCalls <= 0 {
		c.GlobalMaxCalls = DefaultGlobalMaxCalls
	}
	if c.GlobalMaxTokens <= 0 {
		c.GlobalMaxTokens = DefaultGlobalMaxTokens
	}
	if c.MaxConcurrentRequests <= 0 {
		c.MaxConcurrentRequests = DefaultMaxConcurrentRequests
	}
	return c
}

// Available reports whether the LLM opponent can be used: base URL, key and
// model are all set.
func (c Config) Available() bool {
	return c.BaseURL != "" && c.APIKey != "" && c.Model != ""
}

// Redacted returns a log-safe description. The API key never appears.
func (c Config) Redacted() string {
	key := "unset"
	if c.APIKey != "" {
		key = "set"
	}
	return fmt.Sprintf("llm.Config{baseURL=%q model=%q apiKey=%s timeout=%s maxCalls=%d maxTokensPerCall=%d maxTotalTokens=%d maxPromptBytes=%d globalCalls=%d globalTokens=%d concurrent=%d}",
		c.BaseURL, c.Model, key, c.RequestTimeout, c.MaxCallsPerWorld, c.MaxTokensPerCall, c.MaxTotalTokensPerWorld,
		c.MaxPromptBytes, c.GlobalMaxCalls, c.GlobalMaxTokens, c.MaxConcurrentRequests)
}

// String implements fmt.Stringer with the redacted form.
func (c Config) String() string { return c.Redacted() }

// GoString keeps %#v from printing the key.
func (c Config) GoString() string { return c.Redacted() }

// Usage is the cumulative LLM consumption of one world. Unreported is set
// only on a value returned by Client.Complete when the provider answered
// without token usage; the caller then charges the full reservation.
type Usage struct {
	Calls            int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Unreported       bool
}

// ErrBudgetExhausted means a call or token limit (per world or global) cannot
// cover the next call; the caller must fall back to the rule-based manager
// (reason budget_exhausted).
var ErrBudgetExhausted = errors.New("llm: budget exhausted")

// ErrPromptTooLarge means the prompt exceeds LLM_MAX_PROMPT_BYTES. It is
// reported as budget_exhausted and no request is made.
var ErrPromptTooLarge = fmt.Errorf("%w: prompt exceeds %s", ErrBudgetExhausted, EnvMaxPromptBytes)

// EstimatePromptTokens is a deliberately conservative upper estimate of the
// prompt tokens of the exact request text: ceil(bytes/3), at least 1 token
// per 3 bytes (typical English text and JSON need about 1 token per 4 bytes).
func EstimatePromptTokens(system, user string) int {
	return (len(system) + len(user) + 2) / 3
}

// ReservationTokens is the amount reserved before a call: the estimated
// prompt tokens plus the completion cap max_tokens.
func (c Config) ReservationTokens(system, user string) int {
	return EstimatePromptTokens(system, user) + c.MaxTokensPerCall
}

// Tracker enforces a call and token limit. It is safe for concurrent use.
// The app layer keeps one Tracker per world and one global daily Tracker.
type Tracker struct {
	mu          sync.Mutex
	maxCalls    int
	maxTotalTok int
	inflightTok int // tokens reserved by calls not yet settled
	usage       Usage
	overshoot   int // cumulative tokens reported above their reservations
	now         func() time.Time
	day         int64 // UTC day number of usage, daily trackers only
}

// NewTracker returns a per-world Tracker with the limits of cfg.
func NewTracker(cfg Config) *Tracker {
	return &Tracker{maxCalls: cfg.MaxCallsPerWorld, maxTotalTok: cfg.MaxTotalTokensPerWorld}
}

// NewGlobalTracker returns the server-wide Tracker. Its usage resets when the
// UTC calendar day of now changes. now may be nil (time.Now).
func NewGlobalTracker(cfg Config, now func() time.Time) *Tracker {
	if now == nil {
		now = time.Now
	}
	t := &Tracker{maxCalls: cfg.GlobalMaxCalls, maxTotalTok: cfg.GlobalMaxTokens, now: now}
	t.day = t.today()
	return t
}

func (t *Tracker) today() int64 { return t.now().UTC().Unix() / 86400 }

// rollLocked starts a new day's window. In-flight reservations carry over.
func (t *Tracker) rollLocked() {
	if t.now == nil {
		return
	}
	if d := t.today(); d != t.day {
		t.day = d
		t.usage = Usage{}
	}
}

// Restore sets the cumulative usage, for rebuilding a Tracker after restart.
func (t *Tracker) Restore(u Usage) {
	t.mu.Lock()
	t.rollLocked()
	t.usage = Usage{Calls: u.Calls, PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, TotalTokens: u.TotalTokens}
	t.mu.Unlock()
}

// Usage returns the usage so far.
func (t *Tracker) Usage() Usage {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rollLocked()
	return t.usage
}

// Limits returns the maximum calls and total tokens.
func (t *Tracker) Limits() (calls, totalTokens int) { return t.maxCalls, t.maxTotalTok }

// Overshoot returns the cumulative tokens providers reported above what was
// reserved. Those tokens are included in Usage.
func (t *Tracker) Overshoot() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.overshoot
}

// Remaining returns the calls and tokens still available, not counting
// in-flight reservations' calls (already counted) but counting their tokens.
func (t *Tracker) Remaining() (calls, tokens int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rollLocked()
	return max(t.maxCalls-t.usage.Calls, 0), max(t.maxTotalTok-t.usage.TotalTokens-t.inflightTok, 0)
}

// Reserve counts one call and reserves tokens, or returns ErrBudgetExhausted
// without counting when the call limit is reached or used + in-flight +
// tokens would exceed the token limit. Every successful Reserve must be
// settled by exactly one Settle or Release.
func (t *Tracker) Reserve(tokens int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rollLocked()
	if t.usage.Calls >= t.maxCalls || t.usage.TotalTokens+t.inflightTok+tokens > t.maxTotalTok {
		return ErrBudgetExhausted
	}
	t.usage.Calls++
	t.inflightTok += tokens
	return nil
}

// Settle replaces a reservation of reserved tokens with the actual usage
// (never less than reported). Actual above the reservation is an overshoot:
// it is accounted in full and recorded, and since the next Reserve needs
// room above used tokens, an exceeded limit blocks all further calls.
func (t *Tracker) Settle(reserved int, u Usage) {
	t.mu.Lock()
	t.inflightTok = max(t.inflightTok-reserved, 0)
	total := u.PromptTokens + u.CompletionTokens
	t.usage.PromptTokens += u.PromptTokens
	t.usage.CompletionTokens += u.CompletionTokens
	t.usage.TotalTokens += total
	if total > reserved {
		t.overshoot += total - reserved
	}
	t.mu.Unlock()
}

// Release undoes a reservation for a call that was never made.
func (t *Tracker) Release(reserved int) {
	t.mu.Lock()
	t.inflightTok = max(t.inflightTok-reserved, 0)
	if t.usage.Calls > 0 {
		t.usage.Calls--
	}
	t.mu.Unlock()
}

// Reservation is a held call and token reservation against a world Tracker
// and, when present, the global Tracker.
type Reservation struct {
	world, global *Tracker
	Tokens        int // reserved: prompt estimate + completion cap
	promptEst     int
}

// Reserve reserves tokens against global (may be nil) and then world,
// atomically with respect to each: if either refuses, nothing is held.
func Reserve(world, global *Tracker, promptEst, tokens int) (*Reservation, error) {
	if global != nil {
		if err := global.Reserve(tokens); err != nil {
			return nil, err
		}
	}
	if err := world.Reserve(tokens); err != nil {
		if global != nil {
			global.Release(tokens)
		}
		return nil, err
	}
	return &Reservation{world: world, global: global, Tokens: tokens, promptEst: promptEst}, nil
}

// Settle charges actual usage and returns what was charged. When the provider reported none
// (u.Unreported) the whole reservation is charged: the prompt estimate as
// prompt tokens and the rest as completion tokens.
func (r *Reservation) Settle(u Usage) Usage {
	if u.Unreported {
		u = Usage{PromptTokens: r.promptEst, CompletionTokens: r.Tokens - r.promptEst}
	}
	u.TotalTokens = u.PromptTokens + u.CompletionTokens
	r.world.Settle(r.Tokens, u)
	if r.global != nil {
		r.global.Settle(r.Tokens, u)
	}
	return u
}

// Release frees the reservation of a call that made no request.
func (r *Reservation) Release() {
	r.world.Release(r.Tokens)
	if r.global != nil {
		r.global.Release(r.Tokens)
	}
}

// FallbackReason maps an error from Manager.Propose to the recorded fallback
// reason of security.md: timeout, http_error, invalid_json, schema_violation
// or budget_exhausted. Unknown errors map to http_error.
func FallbackReason(err error) string {
	switch {
	case errors.Is(err, ErrBudgetExhausted):
		return "budget_exhausted"
	case errors.Is(err, ErrTimeout):
		return "timeout"
	case errors.Is(err, ErrInvalidJSON):
		return "invalid_json"
	case errors.Is(err, ErrSchemaViolation):
		return "schema_violation"
	default:
		return "http_error"
	}
}
