package googleauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend-loanbanking/internal/auth"
)

type userProviderStub struct{}

func (userProviderStub) CurrentUser(_ context.Context, token string) (auth.User, error) {
	if token == "local-jwt" {
		return auth.User{ID: 42, IsActive: true}, nil
	}
	return auth.User{}, auth.ErrInvalidCredentials
}
func handlerFixture() (*Handler, *Service, *memoryRepository) {
	service, repo, _ := fixture()
	return NewHandler(service, userProviderStub{}, false, slog.New(slog.NewTextHandler(io.Discard, nil))), service, repo
}

func TestHandlerRedirectCookieAndCallbackJSON(t *testing.T) {
	handler, _, _ := handlerFixture()
	response := httptest.NewRecorder()
	handler.Start(response, httptest.NewRequest("GET", "http://127.0.0.1:8080/auth/google", nil))
	if response.Code != 302 || !strings.HasPrefix(response.Header().Get("Location"), "https://accounts.google.com/") {
		t.Fatal("missing Google redirect")
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].MaxAge != 600 {
		t.Fatal("unsafe OAuth cookie")
	}
	location := response.Result().Header.Get("Location")
	startRequest, _ := http.NewRequest("GET", location, nil)
	callback := httptest.NewRequest("GET", "/auth/google/callback?state="+startRequest.URL.Query().Get("state")+"&code=test-code", nil)
	callback.AddCookie(cookies[0])
	result := httptest.NewRecorder()
	handler.Callback(result, callback)
	if result.Code != 200 || !strings.Contains(result.Body.String(), "access_token") || strings.Contains(result.Body.String(), "never-expose") {
		t.Fatalf("callback failed: %s", result.Body.String())
	}
	if result.Result().Cookies()[0].MaxAge != -1 || result.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("state cookie/referrer not cleared")
	}
}

func TestHandlerRejectsTamperingAndSupportsExplicitLink(t *testing.T) {
	handler, _, repo := handlerFixture()
	req := httptest.NewRequest("GET", "/auth/google?format=json", nil)
	req.Header.Set("Authorization", "Bearer local-jwt")
	response := httptest.NewRecorder()
	handler.Start(response, req)
	var body struct {
		Data struct {
			URL string `json:"authorization_url"`
		} `json:"data"`
	}
	if json.Unmarshal(response.Body.Bytes(), &body) != nil || response.Code != 200 || body.Data.URL == "" {
		t.Fatal("JSON authorization URL missing")
	}
	for _, flow := range repo.flows {
		if flow.LinkUserID != 42 {
			t.Fatal("link owner not from authenticated account")
		}
	}
	for _, query := range []string{"state=x&state=y&code=z", "state=x&code=a&code=b", "state=x;bad=1", "state=x"} {
		result := httptest.NewRecorder()
		handler.Callback(result, httptest.NewRequest("GET", "/auth/google/callback?"+query, nil))
		if result.Code != 400 {
			t.Fatal("callback tampering accepted")
		}
	}
	secure := NewHandler(nil, nil, true, slog.Default())
	if secure.cookieName() != "__Host-google_oauth_state" || !secure.cookie("value", false).Secure {
		t.Fatal("HTTPS cookie not protected")
	}
	for _, run := range []func(http.ResponseWriter, *http.Request){secure.Start, secure.Callback} {
		result := httptest.NewRecorder()
		run(result, httptest.NewRequest("GET", "/auth/google", nil))
		if result.Code != 503 {
			t.Fatal("missing config not explicit")
		}
	}
}

func TestGoogleErrorsDoNotLeakDetails(t *testing.T) {
	handler, _, _ := handlerFixture()
	for _, tc := range []struct {
		err    error
		status int
	}{
		{ErrInvalidState, 400}, {ErrConsentDenied, 400}, {ErrInvalidIdentity, 401}, {ErrLinkRequired, 409}, {ErrAccountConflict, 409}, {ErrProvider, 502}, {errors.New("private-client-secret"), 500},
	} {
		response := httptest.NewRecorder()
		handler.serviceError(response, httptest.NewRequest("GET", "/auth/google/callback?code=private-code", nil), tc.err)
		if response.Code != tc.status || strings.Contains(response.Body.String(), "private-") {
			t.Fatalf("unsafe error %d %s", response.Code, response.Body.String())
		}
	}
}
