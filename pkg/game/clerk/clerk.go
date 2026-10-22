package clerk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	userAgent      = "rsned/spacemolt-clerk/0.0.1"
	defaultBaseURL = "https://game.spacemolt.com"

	// maxErrBody caps HTTPError.Body so an HTML error page from a proxy cannot
	// bloat logs or error strings.
	maxErrBody = 1024
	// maxRespBody bounds what we read at all; a player list is far smaller.
	maxRespBody = 1 << 20
)

// Player is one account player as reported by the Clerk-gated players endpoint.
type Player struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Empire   string `json:"empire"`
	Hidden   bool   `json:"hidden"`
}

// Token is a single-use websocket token. ExpiresIn is already a duration;
// the wire value is seconds.
type Token struct {
	Value     string
	ExpiresIn time.Duration
}

// HTTPError is a non-2xx response. RetryAfter is zero when the server gave no
// hint. The request's Authorization header is never copied into it, so the
// error is safe to log.
type HTTPError struct {
	Status     int
	Method     string
	URL        string
	RetryAfter time.Duration
	Body       string // capped at 1024 bytes
}

// Error reports method, path (no scheme or host, so test-server ports do not
// leak into messages), status and any retry hint.
func (e *HTTPError) Error() string {
	path := e.URL
	if u, err := url.Parse(e.URL); err == nil && u.Path != "" {
		path = u.EscapedPath()
	}
	msg := fmt.Sprintf("clerk: %s %s -> %d", e.Method, path, e.Status)
	if e.RetryAfter > 0 {
		msg += fmt.Sprintf(" (retry_after %s)", e.RetryAfter)
	}
	return msg
}

// RateLimited reports whether the server answered 429.
func (e *HTTPError) RateLimited() bool { return e.Status == http.StatusTooManyRequests }

// Option configures a Source.
type Option func(*Source)

// WithHTTPClient replaces the default client (30s timeout).
func WithHTTPClient(c *http.Client) Option { return func(s *Source) { s.client = c } }

// WithBaseURL overrides the API origin; a trailing slash is stripped.
func WithBaseURL(u string) Option {
	return func(s *Source) { s.baseURL = strings.TrimRight(u, "/") }
}

// Source talks to the Clerk-gated endpoints with one API key.
type Source struct {
	key     string
	baseURL string
	client  *http.Client
}

// NewSource builds a Source. The key is only ever sent as a bearer header.
func NewSource(key string, opts ...Option) *Source {
	s := &Source{
		key:     key,
		baseURL: defaultBaseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

type playersResponse struct {
	RegistrationCode string   `json:"registration_code"`
	Players          []Player `json:"players"`
}

// ListPlayers returns every player on the account.
func (s *Source) ListPlayers(ctx context.Context) ([]Player, error) {
	r, err := s.players(ctx)
	if err != nil {
		return nil, err
	}
	return r.Players, nil
}

// RegistrationCode returns the account's registration code.
func (s *Source) RegistrationCode(ctx context.Context) (string, error) {
	r, err := s.players(ctx)
	if err != nil {
		return "", err
	}
	return r.RegistrationCode, nil
}

func (s *Source) players(ctx context.Context) (playersResponse, error) {
	var r playersResponse
	body, err := s.do(ctx, http.MethodGet, "/api/players")
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return r, fmt.Errorf("clerk: decode players: %w", err)
	}
	return r, nil
}

// MintWSToken mints a single-use websocket token for playerID. An empty token
// in a 2xx response is an error: handing "" to a login would just burn a
// connection attempt.
func (s *Source) MintWSToken(ctx context.Context, playerID string) (Token, error) {
	body, err := s.do(ctx, http.MethodPost, "/api/player/"+url.PathEscape(playerID)+"/ws-token")
	if err != nil {
		return Token{}, err
	}
	var r struct {
		Token     string  `json:"token"`
		ExpiresIn float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return Token{}, fmt.Errorf("clerk: decode ws-token: %w", err)
	}
	if r.Token == "" {
		return Token{}, errors.New("clerk: ws-token response had no token")
	}
	return Token{Value: r.Token, ExpiresIn: time.Duration(r.ExpiresIn * float64(time.Second))}, nil
}

// do performs one request and returns the body of a 2xx response. Anything
// else becomes an *HTTPError.
func (s *Source) do(ctx context.Context, method, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("clerk: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := s.client.Do(req)
	if err != nil {
		// net/http errors embed the URL but not headers, so the key is safe.
		return nil, fmt.Errorf("clerk: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBody))
	if err != nil {
		return nil, fmt.Errorf("clerk: read %s %s: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		he := &HTTPError{
			Status:     resp.StatusCode,
			Method:     method,
			URL:        req.URL.String(),
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), data),
			Body:       string(data[:min(len(data), maxErrBody)]),
		}
		return nil, he
	}
	return data, nil
}

// parseRetryAfter prefers the Retry-After header (integer seconds), then a
// numeric retry_after in the JSON body at the root or under "error".
func parseRetryAfter(header string, body []byte) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	var b struct {
		RetryAfter any `json:"retry_after"`
		Error      struct {
			RetryAfter any `json:"retry_after"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &b) != nil {
		return 0
	}
	for _, v := range []any{b.RetryAfter, b.Error.RetryAfter} {
		if f, ok := v.(float64); ok && f > 0 {
			return time.Duration(f * float64(time.Second))
		}
	}
	return 0
}
