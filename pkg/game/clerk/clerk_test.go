package clerk

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const specBody = `{"registration_code":"379abc","players":[{"id":"573b","username":"battle_bot1","empire":"nebula","hidden":false}]}`

func newTestSource(t *testing.T, h http.HandlerFunc) *Source {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewSource("k-test", WithBaseURL(srv.URL+"/"))
}

func TestListPlayers_ParsesSpecShape(t *testing.T) {
	var gotAuth, gotAccept, gotUA, gotPath string
	s := newTestSource(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotUA = r.Header.Get("User-Agent")
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(specBody))
	})
	got, err := s.ListPlayers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Player{ID: "573b", Username: "battle_bot1", Empire: "nebula", Hidden: false}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v, want [%+v]", got, want)
	}
	if gotAuth != "Bearer k-test" || gotAccept != "application/json" || gotUA != userAgent {
		t.Errorf("headers: auth=%q accept=%q ua=%q", gotAuth, gotAccept, gotUA)
	}
	if gotPath != "/api/registration-code" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestRegistrationCode(t *testing.T) {
	s := newTestSource(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(specBody))
	})
	got, err := s.RegistrationCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "379abc" {
		t.Errorf("got %q", got)
	}
}

func TestMintWSToken_ReturnsTokenAndExpiry(t *testing.T) {
	var method, escaped string
	s := newTestSource(t, func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		escaped = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"token":"t","expires_in":300}`))
	})
	got, err := s.MintWSToken(context.Background(), "p/1")
	if err != nil {
		t.Fatal(err)
	}
	if got != (Token{Value: "t", ExpiresIn: 300 * time.Second}) {
		t.Errorf("got %+v", got)
	}
	if method != http.MethodPost || escaped != "/api/player/p%2F1/ws-token" {
		t.Errorf("method=%s path=%s", method, escaped)
	}
}

func TestMintWSToken_429HeaderBecomesHTTPError(t *testing.T) {
	s := newTestSource(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := s.MintWSToken(context.Background(), "x")
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("err = %v, want *HTTPError", err)
	}
	if he.Status != 429 || he.RetryAfter != 7*time.Second || !he.RateLimited() {
		t.Errorf("got %+v", he)
	}
	if strings.Contains(err.Error(), "k-test") {
		t.Errorf("error leaks key: %v", err)
	}
	if want := "clerk: POST /api/player/x/ws-token -> 429 (retry_after 7s)"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestMintWSToken_429BodyRetryAfter(t *testing.T) {
	for name, body := range map[string]string{
		"nested": `{"error":{"code":"rate_limited","retry_after":12}}`,
		"root":   `{"retry_after":12}`,
	} {
		t.Run(name, func(t *testing.T) {
			s := newTestSource(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(body))
			})
			_, err := s.MintWSToken(context.Background(), "x")
			var he *HTTPError
			if !errors.As(err, &he) {
				t.Fatalf("err = %v", err)
			}
			if he.RetryAfter != 12*time.Second {
				t.Errorf("RetryAfter = %v", he.RetryAfter)
			}
		})
	}
}

func TestMintWSToken_EmptyTokenIsError(t *testing.T) {
	s := newTestSource(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"token":""}`))
	})
	_, err := s.MintWSToken(context.Background(), "x")
	if err == nil {
		t.Fatal("want error")
	}
	var he *HTTPError
	if errors.As(err, &he) {
		t.Errorf("want non-HTTPError, got %v", he)
	}
}

func TestMintWSToken_HTMLBodyIsBounded(t *testing.T) {
	s := newTestSource(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("<html>" + strings.Repeat("x", 5*1024) + "</html>"))
	})
	_, err := s.MintWSToken(context.Background(), "x")
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("err = %v", err)
	}
	if len(he.Body) == 0 || len(he.Body) > 1024 {
		t.Errorf("len(Body) = %d", len(he.Body))
	}
}
