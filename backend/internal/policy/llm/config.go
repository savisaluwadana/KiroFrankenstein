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
	return fmt.Sprintf("llm.Config{baseURL=%q model=%q apiKey=%s timeout=%s maxCalls=%d maxTokensPerCall=%d maxTotalTokens=%d}",
		c.BaseURL, c.Model, key, c.RequestTimeout, c.MaxCallsPerWorld, c.MaxTokensPerCall, c.MaxTotalTokensPerWorld)
}

// String implements fmt.Stringer with the redacted form.
func (c Config) String() string { return c.Redacted() }

// GoString keeps %#v from printing the key.
func (c Config) GoString() string { return c.Redacted() }

// Usage is the cumulative LLM consumption of one world.
type Usage struct {
	Calls            int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// ErrBudgetExhausted means the per-world call or token limit is spent; the
// caller must fall back to the rule-based manager (reason budget_exhausted).
var ErrBudgetExhausted = errors.New("llm: per-world budget exhausted")

// Tracker enforces the per-world call and token limits. It is safe for
// concurrent use. The app layer keeps one Tracker per world (or restores it
// from recorded usage with Restore) and passes it to NewManager.
type Tracker struct {
	mu          sync.Mutex
	maxCalls    int
	maxTotalTok int
	perCallTok  int // assumed worst-case tokens of a call still in flight
	inflight    int // reserved calls whose usage is not yet recorded
	usage       Usage
}

// NewTracker returns a Tracker with the limits of cfg.
func NewTracker(cfg Config) *Tracker {
	return &Tracker{maxCalls: cfg.MaxCallsPerWorld, maxTotalTok: cfg.MaxTotalTokensPerWorld, perCallTok: cfg.MaxTokensPerCall}
}

// Restore sets the cumulative usage, for rebuilding a Tracker after restart.
func (t *Tracker) Restore(u Usage) {
	t.mu.Lock()
	t.usage = u
	t.mu.Unlock()
}

// Usage returns the usage so far.
func (t *Tracker) Usage() Usage {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.usage
}

// Limits returns the maximum calls and total tokens.
func (t *Tracker) Limits() (calls, totalTokens int) { return t.maxCalls, t.maxTotalTok }

// Reserve counts one call, or returns ErrBudgetExhausted without counting
// when either limit is already reached. Calls already reserved but not yet
// recorded are assumed to spend MaxTokensPerCall each, so concurrent
// reservations cannot all pass a token limit that only the first one fits
// (shortcut: a provider that reports more than that per call can still
// overshoot the token limit by the excess; the call limit is exact).
// Every successful Reserve must be settled by exactly one Record, with a zero
// Usage when no request was made.
func (t *Tracker) Reserve() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.usage.Calls >= t.maxCalls || t.usage.TotalTokens+t.inflight*t.perCallTok >= t.maxTotalTok {
		return ErrBudgetExhausted
	}
	t.usage.Calls++
	t.inflight++
	return nil
}

// Record adds provider-reported token usage and settles one reservation.
func (t *Tracker) Record(u Usage) {
	t.mu.Lock()
	if t.inflight > 0 {
		t.inflight--
	}
	t.usage.PromptTokens += u.PromptTokens
	t.usage.CompletionTokens += u.CompletionTokens
	t.usage.TotalTokens += u.TotalTokens
	t.mu.Unlock()
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
