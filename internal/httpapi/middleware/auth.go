package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"backend-loanbanking/internal/auth"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

type UserProvider interface {
	CurrentUser(context.Context, string) (auth.User, error)
}

type userIDKey struct{}

func UserID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(userIDKey{}).(int64)
	return id, ok && id > 0
}

// RequireActiveUser verifies JWT and reads the current account from the database.
func RequireActiveUser(provider UserProvider, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			parts := strings.Fields(request.Header.Get("Authorization"))
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) > 4096 {
				writeAuthError(writer, http.StatusUnauthorized)
				return
			}
			ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
			defer cancel()
			user, err := provider.CurrentUser(ctx, parts[1])
			cancel()
			if err != nil {
				if errors.Is(err, auth.ErrInvalidCredentials) {
					writeAuthError(writer, http.StatusUnauthorized)
					return
				}
				logger.Error("customer authentication failed", "request_id", chimiddleware.GetReqID(ctx))
				writeAuthError(writer, http.StatusInternalServerError)
				return
			}
			if user.ID <= 0 || !user.IsActive {
				writeAuthError(writer, http.StatusUnauthorized)
				return
			}
			// Authentication has its own deadline. Each handler controls the duration
			// of its operation, so an upload is not cut off by this five-second check.
			next.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), userIDKey{}, user.ID)))
		})
	}
}

func writeAuthError(writer http.ResponseWriter, status int) {
	code, message := "unauthorized", "Invalid credentials or access token"
	if status == http.StatusUnauthorized {
		writer.Header().Set("WWW-Authenticate", "Bearer")
	} else {
		code, message = "internal_error", "An internal error occurred"
	}
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
