package googleauth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"backend-loanbanking/internal/auth"
	"github.com/go-chi/chi/v5/middleware"
)

type LoginService interface {
	Start(context.Context, int64) (string, string, error)
	Complete(context.Context, string, string, string, string) (auth.LoginResult, error)
}
type UserProvider interface {
	CurrentUser(context.Context, string) (auth.User, error)
}
type Handler struct {
	service LoginService
	users   UserProvider
	secure  bool
	logger  *slog.Logger
}

func NewHandler(service LoginService, users UserProvider, secure bool, logger *slog.Logger) *Handler {
	return &Handler{service: service, users: users, secure: secure, logger: logger}
}

func (handler *Handler) cookieName() string {
	if handler.secure {
		return "__Host-google_oauth_state"
	}
	return "google_oauth_state"
}
func (handler *Handler) cookie(value string, clear bool) *http.Cookie {
	cookie := &http.Cookie{Name: handler.cookieName(), Value: value, Path: "/", HttpOnly: true, Secure: handler.secure, SameSite: http.SameSiteLaxMode, MaxAge: int(FlowLifetime.Seconds()), Expires: time.Now().Add(FlowLifetime)}
	if clear {
		cookie.MaxAge = -1
		cookie.Expires = time.Unix(1, 0)
	}
	return cookie
}

func (handler *Handler) Start(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Referrer-Policy", "no-referrer")
	if handler.service == nil {
		respond(writer, 503, "google_oauth_unavailable", "Google login is not configured")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
	defer cancel()
	var linkUserID int64
	if header := request.Header.Get("Authorization"); header != "" {
		parts := strings.Fields(header)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) > 4096 {
			respond(writer, 401, "unauthorized", "Invalid credentials or access token")
			return
		}
		user, err := handler.users.CurrentUser(ctx, parts[1])
		if err != nil {
			handler.serviceError(writer, request, err)
			return
		}
		if user.ID <= 0 || !user.IsActive {
			handler.serviceError(writer, request, auth.ErrInvalidCredentials)
			return
		}
		linkUserID = user.ID
	}
	url, binding, err := handler.service.Start(ctx, linkUserID)
	if err != nil {
		handler.serviceError(writer, request, err)
		return
	}
	http.SetCookie(writer, handler.cookie(binding, false))
	if request.URL.Query().Get("format") == "json" {
		writeJSON(writer, 200, map[string]any{"data": map[string]string{"authorization_url": url}})
		return
	}
	http.Redirect(writer, request, url, http.StatusFound)
}

func (handler *Handler) Callback(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Referrer-Policy", "no-referrer")
	if handler.service == nil {
		respond(writer, 503, "google_oauth_unavailable", "Google login is not configured")
		return
	}
	if len(request.URL.RawQuery) > 8192 {
		handler.serviceError(writer, request, ErrInvalidState)
		return
	}
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil || len(query["state"]) != 1 || len(query["code"]) > 1 || len(query["error"]) > 1 {
		handler.serviceError(writer, request, ErrInvalidState)
		return
	}
	var binding string
	count := 0
	for _, cookie := range request.Cookies() {
		if cookie.Name == handler.cookieName() {
			binding = cookie.Value
			count++
		}
	}
	if count != 1 {
		handler.serviceError(writer, request, ErrInvalidState)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	result, err := handler.service.Complete(ctx, query.Get("state"), binding, query.Get("code"), query.Get("error"))
	if !errors.Is(err, ErrInvalidState) {
		http.SetCookie(writer, handler.cookie("", true))
	}
	if err != nil {
		handler.serviceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": result})
}

func (handler *Handler) serviceError(writer http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalidState):
		respond(writer, 400, "invalid_oauth_state", "Google login expired or browser state is invalid; start again")
	case errors.Is(err, ErrConsentDenied):
		respond(writer, 400, "google_login_cancelled", "Google login was cancelled")
	case errors.Is(err, ErrInvalidIdentity), errors.Is(err, auth.ErrInvalidCredentials):
		respond(writer, 401, "unauthorized", "Google identity or account is invalid")
	case errors.Is(err, ErrLinkRequired):
		respond(writer, 409, "google_link_required", "Sign in with your existing account and link Google first")
	case errors.Is(err, ErrAccountConflict):
		respond(writer, 409, "google_account_conflict", "Google identity cannot be linked to this account")
	case errors.Is(err, ErrProvider):
		respond(writer, 502, "google_provider_unavailable", "Google authentication is temporarily unavailable")
	default:
		handler.logger.Error("Google login failed", "request_id", middleware.GetReqID(request.Context()))
		respond(writer, 500, "internal_error", "An internal error occurred")
	}
}

func respond(writer http.ResponseWriter, status int, code, message string) {
	if status == 401 {
		writer.Header().Set("WWW-Authenticate", "Bearer")
	}
	writeJSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
