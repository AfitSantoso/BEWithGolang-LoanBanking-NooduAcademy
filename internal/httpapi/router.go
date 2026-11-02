package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"backend-loanbanking/internal/auth"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
)

type AuthService interface {
	Login(context.Context, string, string) (auth.LoginResult, error)
	CurrentUser(context.Context, string) (auth.User, error)
}

type handler struct {
	service AuthService
	logger  *slog.Logger
	ping    func(context.Context) error
}

func NewRouter(service AuthService, logger *slog.Logger, ping func(context.Context) error, corsOrigin string) http.Handler {
	handlers := &handler{service: service, logger: logger, ping: ping}
	router := chi.NewRouter()
	router.Use(middleware.RequestID, middleware.Recoverer, middleware.Throttle(32))
	router.Use(securityHeaders, cors(corsOrigin))
	router.Get("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
	})
	router.Get("/readyz", handlers.ready)
	router.Route("/api/v1/auth", func(router chi.Router) {
		router.With(httprate.LimitBy(5, time.Minute, clientIP, httprate.WithLimitHandler(func(writer http.ResponseWriter, request *http.Request) {
			writeError(writer, http.StatusTooManyRequests, "rate_limit_exceeded", "Too many login attempts; try again later")
		}))).Post("/login", handlers.login)
		router.Get("/me", handlers.currentUser)
	})
	router.NotFound(func(writer http.ResponseWriter, request *http.Request) {
		writeError(writer, http.StatusNotFound, "not_found", "Endpoint not found")
	})
	router.MethodNotAllowed(func(writer http.ResponseWriter, request *http.Request) {
		writeError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	})
	return router
}

func (handlers *handler) login(writer http.ResponseWriter, request *http.Request) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 4096)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decoder.Decode(&input); err != nil {
		writeDecodeError(writer, err)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeDecodeError(writer, err)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
	defer cancel()
	result, err := handlers.service.Login(ctx, input.Email, input.Password)
	if err != nil {
		handlers.serviceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": result})
}

func (handlers *handler) currentUser(writer http.ResponseWriter, request *http.Request) {
	parts := strings.Fields(request.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) > 4096 {
		writeUnauthorized(writer)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
	defer cancel()
	user, err := handlers.service.CurrentUser(ctx, parts[1])
	if err != nil {
		handlers.serviceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": user})
}

func (handlers *handler) ready(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
	defer cancel()
	if err := handlers.ping(ctx); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "not_ready", "Database unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
}

func (handlers *handler) serviceError(writer http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidInput):
		writeError(writer, http.StatusBadRequest, "invalid_input", "Provide a valid email and a password of 1 to 72 bytes")
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeUnauthorized(writer)
	default:
		// Do not log credentials, tokens, request bodies, connection URLs or raw driver errors.
		handlers.logger.Error("authentication request failed", "request_id", middleware.GetReqID(request.Context()))
		writeError(writer, http.StatusInternalServerError, "internal_error", "An internal error occurred")
	}
}

func writeDecodeError(writer http.ResponseWriter, err error) {
	var sizeError *http.MaxBytesError
	if errors.As(err, &sizeError) {
		writeError(writer, http.StatusRequestEntityTooLarge, "body_too_large", "Request body must not exceed 4096 bytes")
		return
	}
	writeError(writer, http.StatusBadRequest, "invalid_json", "Provide exactly one JSON object with email and password")
}

func writeUnauthorized(writer http.ResponseWriter) {
	writer.Header().Set("WWW-Authenticate", "Bearer")
	writeError(writer, http.StatusUnauthorized, "unauthorized", "Invalid credentials or access token")
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(writer, request)
	})
}

func cors(allowedOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Add("Vary", "Origin")
			origin := request.Header.Get("Origin")
			if origin != "" && origin == allowedOrigin {
				writer.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
				writer.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			}
			if request.Method == http.MethodOptions && request.Header.Get("Access-Control-Request-Method") != "" {
				if origin == "" || origin != allowedOrigin {
					writeError(writer, http.StatusForbidden, "origin_not_allowed", "Origin is not allowed")
					return
				}
				writer.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(writer, request)
		})
	}
}

// Only trust the socket peer. Forwarding headers from clients are spoofable.
func clientIP(request *http.Request) (string, error) {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr, nil
	}
	return httprate.CanonicalizeIP(host), nil
}
