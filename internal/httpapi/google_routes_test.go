package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGoogleEndpointsExistWithoutConfiguration(t *testing.T) {
	router := NewRouter(&stubService{}, slog.New(slog.NewTextHandler(io.Discard, nil)), func(context.Context) error { return nil }, "")
	for _, path := range []string{"/auth/google", "/auth/google/callback"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 503 || !strings.Contains(response.Body.String(), "google_oauth_unavailable") || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("missing safe disabled route: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestSetLocalPasswordEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name, token, body string
		status            int
	}{
		{"missing token", "", `{"new_password":"new-password-123"}`, 401},
		{"first local password", "valid", `{"new_password":"new-password-123"}`, 200},
		{"password change", "valid", `{"current_password":"old-password-123","new_password":"new-password-123"}`, 200},
		{"owner injection", "valid", `{"user_id":999,"new_password":"new-password-123"}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := NewRouter(&stubService{}, slog.New(slog.NewTextHandler(io.Discard, nil)), func(context.Context) error { return nil }, "")
			req := httptest.NewRequest("PUT", "/api/v1/auth/password", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("got %d: %s", response.Code, response.Body.String())
			}
		})
	}
}
