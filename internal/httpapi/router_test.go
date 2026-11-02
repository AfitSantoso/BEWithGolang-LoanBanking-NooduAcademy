package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend-loanbanking/internal/auth"
)

type stubService struct {
	err        error
	loginCalls int
}

func (service *stubService) Login(ctx context.Context, email, password string) (auth.LoginResult, error) {
	service.loginCalls++
	return auth.LoginResult{AccessToken: "test-token", TokenType: "Bearer", ExpiresIn: 900, User: auth.User{ID: 42, PasswordHash: "secret-hash"}}, service.err
}

func (service *stubService) CurrentUser(ctx context.Context, token string) (auth.User, error) {
	return auth.User{ID: 42, PasswordHash: "secret-hash"}, service.err
}

func newTestRouter(service *stubService) http.Handler {
	return NewRouter(service, slog.New(slog.NewTextHandler(io.Discard, nil)), func(context.Context) error { return nil }, "http://localhost:3000")
}

func TestLoginRequestValidation(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		body        string
		contentType string
		status      int
	}{
		{"valid", `{"email":"alice@example.com","password":"password-123"}`, "application/json", 200},
		{"missing media type", `{}`, "", 415},
		{"malformed JSON", `{`, "application/json", 400},
		{"unknown field", `{"email":"a@example.com","password":"secret","admin":true}`, "application/json", 400},
		{"trailing JSON", `{} {}`, "application/json", 400},
		{"wrong field type", `{"email":123}`, "application/json", 400},
		{"oversized body", `{"email":"` + strings.Repeat("a", 5000) + `"}`, "application/json", 413},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			service := &stubService{}
			router := newTestRouter(service)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(testCase.body))
			request.Header.Set("Content-Type", testCase.contentType)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != testCase.status {
				t.Fatalf("got %d: %s", response.Code, response.Body.String())
			}
			if testCase.status != 200 && service.loginCalls != 0 {
				t.Fatal("invalid request reached service")
			}
			if strings.Contains(response.Body.String(), "secret-hash") {
				t.Fatal("password hash leaked")
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing no-store header")
			}
		})
	}
}

func TestServiceErrorsAreSafe(t *testing.T) {
	for _, testCase := range []struct {
		err    error
		status int
	}{
		{auth.ErrInvalidCredentials, 401}, {auth.ErrInvalidInput, 400}, {errors.New("postgres://private-password@host"), 500},
	} {
		var logs bytes.Buffer
		router := NewRouter(&stubService{err: testCase.err}, slog.New(slog.NewTextHandler(&logs, nil)), nil, "")
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"a@example.com","password":"password-123"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != testCase.status {
			t.Fatalf("got %d, want %d", response.Code, testCase.status)
		}
		if strings.Contains(response.Body.String()+logs.String(), "private-password") {
			t.Fatal("private error leaked")
		}
	}
}

func TestLoginRateLimitIgnoresForwardedHeaders(t *testing.T) {
	service := &stubService{err: auth.ErrInvalidCredentials}
	router := newTestRouter(service)
	for attempt := range 6 {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"a@example.com","password":"password-123"}`))
		request.RemoteAddr = "192.0.2.1:12345"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Forwarded-For", "198.51.100."+string(rune('1'+attempt)))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if attempt == 5 && (response.Code != 429 || response.Header().Get("Retry-After") == "") {
			t.Fatalf("rate limit bypassed: %d", response.Code)
		}
	}
	if service.loginCalls != 5 {
		t.Fatalf("got %d login calls", service.loginCalls)
	}
}

func TestProtectedEndpoint(t *testing.T) {
	router := newTestRouter(&stubService{})
	for _, authorization := range []string{"", "Basic abc", "Bearer", "Bearer abc extra", "Bearer " + strings.Repeat("a", 4097), "Bearer abc"} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		request.Header.Set("Authorization", authorization)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		want := 401
		if authorization == "Bearer abc" {
			want = 200
		}
		if response.Code != want {
			t.Fatalf("got %d, want %d", response.Code, want)
		}
		if strings.Contains(response.Body.String(), "secret-hash") {
			t.Fatal("password hash leaked")
		}
	}
}

func TestCORSAndReadiness(t *testing.T) {
	router := newTestRouter(&stubService{})
	for _, testCase := range []struct {
		origin string
		status int
	}{{"http://localhost:3000", 204}, {"https://untrusted.example", 403}} {
		request := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
		request.Header.Set("Origin", testCase.origin)
		request.Header.Set("Access-Control-Request-Method", "POST")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != testCase.status {
			t.Fatalf("preflight: got %d", response.Code)
		}
		if testCase.status == 403 && response.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("untrusted origin allowed")
		}
	}
	router = NewRouter(&stubService{}, slog.Default(), func(context.Context) error { return errors.New("private database error") }, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != 503 || strings.Contains(response.Body.String(), "private database error") {
		t.Fatalf("unsafe readiness response: %s", response.Body.String())
	}
}
