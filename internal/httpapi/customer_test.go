package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend-loanbanking/internal/auth"
	"backend-loanbanking/internal/customer"
	"backend-loanbanking/internal/imagehost"
)

type customerAuthStub struct{ stubService }

func (*customerAuthStub) CurrentUser(_ context.Context, token string) (auth.User, error) {
	switch token {
	case "user-a":
		return auth.User{ID: 41, Role: auth.RoleUser, IsActive: true}, nil
	case "user-b":
		return auth.User{ID: 42, Role: auth.RoleUser, IsActive: true}, nil
	case "inactive":
		return auth.User{ID: 41, IsActive: false}, nil
	case "database-error":
		return auth.User{}, errors.New("private-connection-string")
	default:
		return auth.User{}, auth.ErrInvalidCredentials
	}
}

type customerServiceStub struct {
	userID         int64
	calls          int
	err            error
	kind           customer.DocumentKind
	contents       []byte
	uploadDeadline time.Duration
}

func (service *customerServiceStub) Get(_ context.Context, userID int64) (customer.Profile, error) {
	service.userID = userID
	service.calls++
	return customer.Profile{ID: 1, UserID: userID}, service.err
}
func (service *customerServiceStub) Save(_ context.Context, userID int64, _ customer.SaveInput) (customer.Profile, error) {
	service.userID = userID
	service.calls++
	return customer.Profile{ID: 1, UserID: userID}, service.err
}

func (service *customerServiceStub) UploadDocument(ctx context.Context, userID int64, kind customer.DocumentKind, contents []byte) (customer.Profile, error) {
	if err := ctx.Err(); err != nil {
		return customer.Profile{}, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		service.uploadDeadline = time.Until(deadline)
	}
	service.userID = userID
	service.calls++
	service.kind = kind
	service.contents = contents
	return customer.Profile{ID: 1, UserID: userID}, service.err
}

func documentBody(t *testing.T, names []string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, name := range names {
		part, err := writer.CreateFormFile(name, "filename-from-user.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, writer.FormDataContentType()
}

func TestDocumentMultipartOwnershipAndLongerDeadline(t *testing.T) {
	for _, kind := range []string{"ktp_image", "selfie_image", "payslip_image"} {
		service := &customerServiceStub{}
		router := customerRouter(service)
		body, media := documentBody(t, []string{"image"}, []byte("file-bytes"))
		req := httptest.NewRequest("PUT", "/api/v1/customers/me/documents/"+kind+"?user_id=999", body)
		req.Header.Set("Authorization", "Bearer user-b")
		req.Header.Set("Content-Type", media)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != 200 || service.userID != 42 || service.kind != customer.DocumentKind(kind) || string(service.contents) != "file-bytes" {
			t.Fatalf("upload binding failed: %d %s", response.Code, response.Body.String())
		}
		if service.uploadDeadline < 50*time.Second {
			t.Fatal("upload reused expired/five-second authentication context")
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("document response may be cached")
		}
	}
}

func TestDocumentMultipartRejectsInvalidRequests(t *testing.T) {
	for _, tc := range []struct {
		name, kind, token string
		fields            []string
		size, status      int
	}{
		{"no auth", "ktp_image", "", []string{"image"}, 1, 401},
		{"invalid kind", "other", "user-a", []string{"image"}, 1, 400},
		{"wrong file field", "ktp_image", "user-a", []string{"ktp_image"}, 1, 400},
		{"multiple images", "ktp_image", "user-a", []string{"image", "image"}, 1, 400},
		{"unknown extra field", "ktp_image", "user-a", []string{"image", "user_id"}, 1, 400},
		{"missing file", "ktp_image", "user-a", nil, 0, 400},
		{"oversize", "ktp_image", "user-a", []string{"image"}, customer.MaxImageBytes + 1, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &customerServiceStub{}
			router := customerRouter(service)
			body, media := documentBody(t, tc.fields, make([]byte, tc.size))
			req := httptest.NewRequest("PUT", "/api/v1/customers/me/documents/"+tc.kind, body)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			req.Header.Set("Content-Type", media)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status || service.calls != 0 {
				t.Fatalf("got %d calls=%d: %s", response.Code, service.calls, response.Body.String())
			}
		})
	}
	for _, media := range []string{"application/json", "multipart/form-data"} {
		req := httptest.NewRequest("PUT", "/api/v1/customers/me/documents/ktp_image", strings.NewReader("{}"))
		req.Header.Set("Content-Type", media)
		req.Header.Set("Authorization", "Bearer user-a")
		response := httptest.NewRecorder()
		customerRouter(&customerServiceStub{}).ServeHTTP(response, req)
		want := 415
		if media == "multipart/form-data" {
			want = 400
		}
		if response.Code != want {
			t.Fatalf("got %d", response.Code)
		}
	}
}

func TestDocumentErrorsAndPerUserRateLimit(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{customer.ErrInvalidImage, 400, "invalid_image"},
		{customer.ErrNotFound, 404, "customer_not_found"},
		{customer.ErrUploadNotConfigured, 503, "image_upload_unavailable"},
		{imagehost.ErrUploadFailed, 502, "image_upload_failed"},
	} {
		body, media := documentBody(t, []string{"image"}, []byte("bytes"))
		req := httptest.NewRequest("PUT", "/api/v1/customers/me/documents/ktp_image", body)
		req.Header.Set("Content-Type", media)
		req.Header.Set("Authorization", "Bearer user-a")
		response := httptest.NewRecorder()
		customerRouter(&customerServiceStub{err: tc.err}).ServeHTTP(response, req)
		if response.Code != tc.status || !strings.Contains(response.Body.String(), tc.code) {
			t.Fatalf("got %d: %s", response.Code, response.Body.String())
		}
	}
	service := &customerServiceStub{}
	router := customerRouter(service)
	for attempt := 0; attempt < 11; attempt++ {
		body, media := documentBody(t, []string{"image"}, []byte("bytes"))
		req := httptest.NewRequest("PUT", "/api/v1/customers/me/documents/ktp_image", body)
		req.Header.Set("Content-Type", media)
		req.Header.Set("Authorization", "Bearer user-a")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if attempt == 10 && (response.Code != 429 || response.Header().Get("Retry-After") == "") {
			t.Fatal("upload rate limit missing")
		}
	}
	if service.calls != 10 {
		t.Fatal("limited upload reached service")
	}
	body, media := documentBody(t, []string{"image"}, []byte("bytes"))
	req := httptest.NewRequest("PUT", "/api/v1/customers/me/documents/ktp_image", body)
	req.Header.Set("Content-Type", media)
	req.Header.Set("Authorization", "Bearer user-b")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != 200 {
		t.Fatal("one user's limit blocked another")
	}
}

func customerRouter(service *customerServiceStub) http.Handler {
	return NewRouter(&customerAuthStub{}, slog.New(slog.NewTextHandler(io.Discard, nil)), func(context.Context) error { return nil }, "http://localhost:3000", WithCustomerService(service))
}

func TestCustomerRoutesUseAuthenticatedIdentity(t *testing.T) {
	service := &customerServiceStub{}
	router := customerRouter(service)
	for token, wantID := range map[string]int64{"user-a": 41, "user-b": 42} {
		for _, method := range []string{"GET", "PUT"} {
			req := httptest.NewRequest(method, "/api/v1/customers/me?user_id=999", strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != 200 || service.userID != wantID {
				t.Fatalf("%s %s got %d owner %d", token, method, response.Code, service.userID)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("sensitive response may be cached")
			}
		}
	}
	for _, path := range []string{"/api/v1/customers/41", "/api/v1/customers/42"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer user-a")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != 404 {
			t.Fatal("arbitrary customer ID route exposed")
		}
	}
}

func TestCustomerAuthenticationFailsBeforeService(t *testing.T) {
	for _, tc := range []struct {
		header string
		status int
	}{
		{"", 401}, {"Basic abc", 401}, {"Bearer", 401}, {"Bearer invalid", 401},
		{"Bearer inactive", 401}, {"Bearer database-error", 500}, {"Bearer " + strings.Repeat("a", 4097), 401},
	} {
		service := &customerServiceStub{}
		router := customerRouter(service)
		for _, method := range []string{"GET", "PUT"} {
			req := httptest.NewRequest(method, "/api/v1/customers/me", strings.NewReader(`{}`))
			req.Header.Set("Authorization", tc.header)
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status || service.calls != 0 {
				t.Fatalf("authentication bypass: %d calls=%d", response.Code, service.calls)
			}
			if strings.Contains(response.Body.String(), "private-connection-string") {
				t.Fatal("internal error leaked")
			}
			if tc.status == 401 && response.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatal("missing Bearer challenge")
			}
		}
	}
}

func TestCustomerJSONBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, body, media string
		status            int
	}{
		{"null", "null", "application/json", 400},
		{"empty", "", "application/json", 400},
		{"array", "[]", "application/json", 400},
		{"malformed", "{", "application/json", 400},
		{"trailing", "{} {}", "application/json", 400},
		{"owner injection", `{"user_id":999}`, "application/json", 400},
		{"id injection", `{"id":999}`, "application/json", 400},
		{"debt injection", `{"current_total_debt":"0"}`, "application/json", 400},
		{"document injection", `{"ktp_image_path":"/etc/passwd"}`, "application/json", 400},
		{"numeric income", `{"monthly_income":7500000}`, "application/json", 400},
		{"oversize", `{"address":"` + strings.Repeat("a", 8300) + `"}`, "application/json", 413},
		{"wrong media", "{}", "text/plain", 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &customerServiceStub{}
			router := customerRouter(service)
			req := httptest.NewRequest("PUT", "/api/v1/customers/me", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.media)
			req.Header.Set("Authorization", "Bearer user-a")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status || service.calls != 0 {
				t.Fatalf("got %d calls=%d: %s", response.Code, service.calls, response.Body.String())
			}
		})
	}
}

func TestCustomerServiceErrorsAreSafe(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{customer.ErrNotFound, 404, "customer_not_found"},
		{customer.ErrIdentityConflict, 409, "customer_identity_exists"},
		{customer.ErrUnauthenticated, 401, "unauthorized"},
		{&customer.ValidationError{Fields: map[string]string{"nik": "Must contain exactly 16 digits"}}, 400, "invalid_profile"},
		{errors.New("private-personal-data-and-database-secret"), 500, "internal_error"},
	} {
		service := &customerServiceStub{err: tc.err}
		router := customerRouter(service)
		req := httptest.NewRequest("PUT", "/api/v1/customers/me", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer user-a")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != tc.status {
			t.Fatalf("got %d: %s", response.Code, response.Body.String())
		}
		var body struct {
			Error struct {
				Code   string            `json:"code"`
				Fields map[string]string `json:"fields"`
			} `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Error.Code != tc.code {
			t.Fatalf("unexpected body: %s", response.Body.String())
		}
		if tc.code == "invalid_profile" && body.Error.Fields["nik"] == "" {
			t.Fatal("missing field errors")
		}
		if strings.Contains(response.Body.String(), "private-personal-data") {
			t.Fatal("private error leaked")
		}
	}
}

func TestCustomerPUTPreflight(t *testing.T) {
	req := httptest.NewRequest("OPTIONS", "/api/v1/customers/me", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "PUT")
	response := httptest.NewRecorder()
	customerRouter(&customerServiceStub{}).ServeHTTP(response, req)
	if response.Code != 204 || !strings.Contains(response.Header().Get("Access-Control-Allow-Methods"), "PUT") {
		t.Fatal("PUT preflight failed")
	}
}
