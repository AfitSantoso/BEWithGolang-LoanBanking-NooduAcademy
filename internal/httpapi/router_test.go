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
	err           error
	loginCalls    int
	registerCalls int
	roleCalls     int
}

func (service *stubService) Register(ctx context.Context, username, email, password string) (auth.User, error) {
	service.registerCalls++
	return auth.User{ID: 43, Username: username, Email: email, Role: auth.RoleUser, PasswordHash: "secret-hash"}, service.err
}

func (service *stubService) SetUserRole(ctx context.Context, token string, userID int64, role string) (auth.User, error) {
	service.roleCalls++
	return auth.User{ID: userID, Role: role, PasswordHash: "secret-hash"}, service.err
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

func TestRegistrationAndRoleEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body, token, contentType string
		err                                          error
		status                                       int
	}{
		{"register", "POST", "/api/v1/auth/register", `{"username":"alice","email":"alice@example.com","password":"valid-password-123"}`, "", "application/json", nil, 201},
		{"reject public admin", "POST", "/api/v1/auth/register", `{"username":"alice","email":"alice@example.com","password":"valid-password-123","role":"admin"}`, "", "application/json", nil, 400},
		{"duplicate", "POST", "/api/v1/auth/register", `{}`, "", "application/json", auth.ErrUserExists, 409},
		{"invalid input", "POST", "/api/v1/auth/register", `{}`, "", "application/json", auth.ErrInvalidRegistration, 400},
		{"media type", "POST", "/api/v1/auth/register", `{}`, "", "text/plain", nil, 415},
		{"oversize", "POST", "/api/v1/auth/register", `{"username":"` + strings.Repeat("a", 5000) + `"}`, "", "application/json", nil, 413},
		{"role update", "PATCH", "/api/v1/admin/users/43/role", `{"role":"admin"}`, "Bearer valid", "application/json", nil, 200},
		{"missing token", "PATCH", "/api/v1/admin/users/43/role", `{"role":"admin"}`, "", "application/json", nil, 401},
		{"bad token", "PATCH", "/api/v1/admin/users/43/role", `{"role":"admin"}`, "Bearer invalid", "application/json", auth.ErrInvalidCredentials, 401},
		{"user forbidden", "PATCH", "/api/v1/admin/users/43/role", `{"role":"admin"}`, "Bearer valid", "application/json", auth.ErrForbidden, 403},
		{"unknown user", "PATCH", "/api/v1/admin/users/43/role", `{"role":"admin"}`, "Bearer valid", "application/json", auth.ErrUserNotFound, 404},
		{"invalid role", "PATCH", "/api/v1/admin/users/43/role", `{"role":"owner"}`, "Bearer valid", "application/json", auth.ErrInvalidRole, 400},
		{"invalid ID", "PATCH", "/api/v1/admin/users/abc/role", `{"role":"admin"}`, "Bearer valid", "application/json", nil, 400},
		{"zero ID", "PATCH", "/api/v1/admin/users/0/role", `{"role":"admin"}`, "Bearer valid", "application/json", nil, 400},
		{"extra field", "PATCH", "/api/v1/admin/users/43/role", `{"role":"admin","is_active":true}`, "Bearer valid", "application/json", nil, 400},
		{"internal error", "PATCH", "/api/v1/admin/users/43/role", `{"role":"admin"}`, "Bearer valid", "application/json", errors.New("private database error"), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &stubService{err: tc.err}
			router := newTestRouter(service)
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			req.Header.Set("Authorization", tc.token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("got %d: %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "secret-hash") || strings.Contains(response.Body.String(), "private database error") {
				t.Fatal("secret leaked")
			}
			if tc.name == "reject public admin" && service.registerCalls != 0 {
				t.Fatal("role injection reached service")
			}
			if tc.name == "missing token" && service.roleCalls != 0 {
				t.Fatal("missing authentication reached service")
			}
		})
	}
}

func TestRegistrationRateLimitAndPATCHPreflight(t *testing.T) {
	router := newTestRouter(&stubService{})
	for attempt := 0; attempt < 6; attempt++ {
		req := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if attempt == 5 && (response.Code != 429 || response.Header().Get("Retry-After") == "") {
			t.Fatal("registration rate limit missing")
		}
	}
	req := httptest.NewRequest("OPTIONS", "/api/v1/admin/users/43/role", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "PATCH")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != 204 || !strings.Contains(response.Header().Get("Access-Control-Allow-Methods"), "PATCH") {
		t.Fatal("PATCH preflight not allowed")
	}
}
