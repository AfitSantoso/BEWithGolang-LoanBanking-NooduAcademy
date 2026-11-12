package imgbb

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend-loanbanking/internal/imagehost"
)

func TestUploadUsesMultipartAndReturnsOnlyPublicMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.RawQuery != "" {
			t.Error("key must not be in URL")
		}
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Error(err)
			return
		}
		if r.FormValue("key") != "test-secret" || r.FormValue("name") != "customer_7_ktp.png" || r.FormValue("expiration") != "" {
			t.Error("wrong provider parameters")
		}
		file, header, err := r.FormFile("image")
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		body, _ := io.ReadAll(file)
		if string(body) != "image-bytes" || header.Filename != "customer_7_ktp.png" {
			t.Error("wrong file")
		}
		_, _ = io.WriteString(w, `{"success":true,"status":200,"data":{"id":"abc123","url":"https://i.ibb.co/xyz/image.png","delete_url":"private-delete-secret"}}`)
	}))
	defer server.Close()
	client := NewClient("test-secret")
	client.endpoint = server.URL
	image, err := client.Upload(context.Background(), "customer_7_ktp.png", []byte("image-bytes"))
	if err != nil || image.ID != "abc123" || image.URL != "https://i.ibb.co/xyz/image.png" {
		t.Fatalf("unexpected upload: %#v %v", image, err)
	}
}

func TestProviderFailuresAreSanitized(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"invalid key", `{"error":"test-secret"}`, 400},
		{"rate limit", `{"error":"test-secret"}`, 429},
		{"invalid JSON", "test-secret", 200},
		{"unsuccessful", `{"success":false,"status":200}`, 200},
		{"missing status", `{"success":true,"data":{"id":"abc","url":"https://i.ibb.co/x/a.png"}}`, 200},
		{"invalid ID", `{"success":true,"status":200,"data":{"id":"../../bad","url":"https://i.ibb.co/x/a.png"}}`, 200},
		{"wrong host", `{"success":true,"status":200,"data":{"id":"abc","url":"https://attacker.example/a.png"}}`, 200},
		{"HTTP URL", `{"success":true,"status":200,"data":{"id":"abc","url":"http://i.ibb.co/a.png"}}`, 200},
		{"large response", strings.Repeat("a", 1024*1024+1), 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			client := NewClient("test-secret")
			client.endpoint = server.URL
			_, err := client.Upload(context.Background(), "image.png", []byte("image"))
			if !errors.Is(err, imagehost.ErrUploadFailed) || strings.Contains(err.Error(), "test-secret") {
				t.Fatalf("unsafe provider error: %v", err)
			}
		})
	}
}

func TestRedirectAndDeadlineDoNotExposeCredentials(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer server.Close()
	client := NewClient("test-secret")
	client.endpoint = server.URL
	if _, err := client.Upload(context.Background(), "image.png", []byte("image")); !errors.Is(err, imagehost.ErrUploadFailed) || reached {
		t.Fatal("redirect forwarded document/key")
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := client.Upload(ctx, "image.png", []byte("image")); !errors.Is(err, imagehost.ErrUploadFailed) {
		t.Fatal("deadline ignored")
	}
}

func TestExpirationIsOptionalAndBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Error(err)
			return
		}
		if r.FormValue("expiration") != "60" {
			t.Error("missing expiration")
		}
		_, _ = io.WriteString(w, `{"success":true,"status":200,"data":{"id":"test","url":"https://i.ibb.co/test/image.png"}}`)
	}))
	defer server.Close()
	client := NewClient("test-secret", WithExpiration(60))
	client.endpoint = server.URL
	if _, err := client.Upload(context.Background(), "test.png", []byte("bytes")); err != nil {
		t.Fatal(err)
	}
	for _, seconds := range []int{1, 59, 15552001, -1} {
		client = NewClient("test-secret", WithExpiration(seconds))
		client.endpoint = server.URL
		if _, err := client.Upload(context.Background(), "test.png", []byte("bytes")); !errors.Is(err, imagehost.ErrUploadFailed) {
			t.Fatal("invalid expiry accepted")
		}
	}
}
