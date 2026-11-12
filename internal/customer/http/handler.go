package customerhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"time"

	"backend-loanbanking/internal/customer"
	authmiddleware "backend-loanbanking/internal/httpapi/middleware"
	"backend-loanbanking/internal/imagehost"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
)

type Service interface {
	Get(context.Context, int64) (customer.Profile, error)
	Save(context.Context, int64, customer.SaveInput) (customer.Profile, error)
	UploadDocument(context.Context, int64, customer.DocumentKind, []byte) (customer.Profile, error)
}

type Handler struct {
	service Service
	logger  *slog.Logger
}

func NewHandler(service Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

// Routes must be mounted behind RequireActiveUser. Handlers also fail closed
// when called without an authenticated context.
func (handler *Handler) Routes(router chi.Router) {
	router.Get("/me", handler.get)
	router.Put("/me", handler.save)
	router.With(httprate.LimitBy(10, time.Minute, func(request *http.Request) (string, error) {
		id, _ := authmiddleware.UserID(request.Context())
		return strconv.FormatInt(id, 10), nil
	}, httprate.WithLimitHandler(func(writer http.ResponseWriter, request *http.Request) {
		writeError(writer, http.StatusTooManyRequests, "rate_limit_exceeded", "Too many document uploads; try again later", nil)
	}))).Put("/me/documents/{documentType}", handler.uploadDocument)
}

func (handler *Handler) get(writer http.ResponseWriter, request *http.Request) {
	userID, ok := authmiddleware.UserID(request.Context())
	if !ok {
		handler.serviceError(writer, request, customer.ErrUnauthenticated)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
	defer cancel()
	profile, err := handler.service.Get(ctx, userID)
	if err != nil {
		handler.serviceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": profile})
}

func (handler *Handler) save(writer http.ResponseWriter, request *http.Request) {
	userID, ok := authmiddleware.UserID(request.Context())
	if !ok {
		handler.serviceError(writer, request, customer.ErrUnauthenticated)
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json", nil)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 8192)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	// Pointer rejects a top-level JSON null rather than silently using zero values.
	var input *customer.SaveInput
	if err := decoder.Decode(&input); err != nil {
		decodeError(writer, err)
		return
	}
	if input == nil {
		decodeError(writer, errors.New("null input"))
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		decodeError(writer, err)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
	defer cancel()
	profile, err := handler.service.Save(ctx, userID, *input)
	if err != nil {
		handler.serviceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": profile})
}

func (handler *Handler) uploadDocument(writer http.ResponseWriter, request *http.Request) {
	userID, ok := authmiddleware.UserID(request.Context())
	if !ok {
		handler.serviceError(writer, request, customer.ErrUnauthenticated)
		return
	}
	kind := customer.DocumentKind(chi.URLParam(request, "documentType"))
	if !kind.Valid() {
		handler.serviceError(writer, request, customer.ErrInvalidDocument)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 60*time.Second)
	defer cancel()
	request = request.WithContext(ctx)
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		writeError(writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be multipart/form-data with one image file", nil)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, customer.MaxImageBytes+64*1024)
	reader, err := request.MultipartReader()
	if err != nil {
		writeError(writer, 400, "invalid_multipart", "Provide one image file in the image field", nil)
		return
	}
	var contents []byte
	count := 0
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			handler.multipartError(writer, request, err)
			return
		}
		count++
		if count != 1 || part.FormName() != "image" || part.FileName() == "" {
			_ = part.Close()
			writeError(writer, 400, "invalid_multipart", "Provide one image file in the image field", nil)
			return
		}
		contents, err = io.ReadAll(io.LimitReader(part, customer.MaxImageBytes+1))
		_ = part.Close()
		if err != nil {
			handler.multipartError(writer, request, err)
			return
		}
		if len(contents) > customer.MaxImageBytes {
			handler.serviceError(writer, request, customer.ErrImageTooLarge)
			return
		}
	}
	if count != 1 {
		writeError(writer, 400, "invalid_multipart", "Provide one image file in the image field", nil)
		return
	}
	profile, err := handler.service.UploadDocument(ctx, userID, kind, contents)
	if err != nil {
		handler.serviceError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": profile})
}

func (handler *Handler) multipartError(writer http.ResponseWriter, request *http.Request, err error) {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		handler.serviceError(writer, request, customer.ErrImageTooLarge)
		return
	}
	writeError(writer, 400, "invalid_multipart", "Provide one image file in the image field", nil)
}

func (handler *Handler) serviceError(writer http.ResponseWriter, request *http.Request, err error) {
	var validation *customer.ValidationError
	switch {
	case errors.Is(err, customer.ErrInvalidDocument):
		writeError(writer, 400, "invalid_document_type", "Use ktp_image, selfie_image or payslip_image", nil)
	case errors.Is(err, customer.ErrInvalidImage):
		writeError(writer, 400, "invalid_image", "Use a JPEG or PNG image up to 25 megapixels, with each dimension at most 10000 pixels", nil)
	case errors.Is(err, customer.ErrImageTooLarge):
		writeError(writer, 413, "image_too_large", "Image must not exceed 5 MiB", nil)
	case errors.Is(err, customer.ErrUploadNotConfigured):
		writeError(writer, 503, "image_upload_unavailable", "Image upload is not configured", nil)
	case errors.Is(err, imagehost.ErrUploadFailed):
		writeError(writer, 502, "image_upload_failed", "Image provider upload failed; try again later", nil)
	case errors.As(err, &validation):
		writeError(writer, http.StatusBadRequest, "invalid_profile", "Customer profile validation failed", validation.Fields)
	case errors.Is(err, customer.ErrUnauthenticated):
		writer.Header().Set("WWW-Authenticate", "Bearer")
		writeError(writer, http.StatusUnauthorized, "unauthorized", "Invalid credentials or access token", nil)
	case errors.Is(err, customer.ErrNotFound):
		writeError(writer, http.StatusNotFound, "customer_not_found", "Complete your customer profile first", nil)
	case errors.Is(err, customer.ErrIdentityConflict):
		writeError(writer, http.StatusConflict, "customer_identity_exists", "Customer identity is already registered", nil)
	default:
		// Never log request bodies, personal information or raw PostgreSQL errors.
		handler.logger.Error("customer request failed", "request_id", middleware.GetReqID(request.Context()))
		writeError(writer, http.StatusInternalServerError, "internal_error", "An internal error occurred", nil)
	}
}

func decodeError(writer http.ResponseWriter, err error) {
	var sizeError *http.MaxBytesError
	if errors.As(err, &sizeError) {
		writeError(writer, http.StatusRequestEntityTooLarge, "body_too_large", "Request body must not exceed 8192 bytes", nil)
		return
	}
	writeError(writer, http.StatusBadRequest, "invalid_json", "Provide exactly one JSON object with the expected fields", nil)
}

func writeError(writer http.ResponseWriter, status int, code, message string, fields map[string]string) {
	errorBody := map[string]any{"code": code, "message": message}
	if len(fields) > 0 {
		errorBody["fields"] = fields
	}
	writeJSON(writer, status, map[string]any{"error": errorBody})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
