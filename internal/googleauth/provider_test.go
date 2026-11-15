package googleauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
)

// Exchange requests are handled in memory: no listener, network or Google account.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestGoogleProviderVerifiesSignedIdentityAndPKCE(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"valid", "wrong signature", "expired", "wrong audience", "wrong issuer", "wrong nonce", "wrong azp", "missing id token", "provider unavailable", "code rejected"} {
		t.Run(scenario, func(t *testing.T) {
			claims := jwt.MapClaims{"iss": "https://accounts.google.com", "sub": "123456789", "aud": "test-client", "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(), "nonce": "test-nonce", "email": "alice@gmail.com", "email_verified": true}
			switch scenario {
			case "expired":
				claims["exp"] = time.Now().Add(-time.Hour).Unix()
			case "wrong audience":
				claims["aud"] = "another-client"
			case "wrong issuer":
				claims["iss"] = "https://attacker.example"
			case "wrong nonce":
				claims["nonce"] = "another-nonce"
			case "wrong azp":
				claims["azp"] = "another-client"
			}
			raw, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "wrong signature" {
				raw, _ = jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("untrusted"))
			}
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Error("code exchange must be POST")
				}
				_ = r.ParseForm()
				if r.Form.Get("client_secret") != "test-secret" || r.Form.Get("code_verifier") != "test-verifier" || r.URL.RawQuery != "" {
					t.Error("incorrect code exchange or credential leakage")
				}
				w.Header().Set("Content-Type", "application/json")
				if scenario == "provider unavailable" {
					w.WriteHeader(503)
					_, _ = w.Write([]byte("private-secret-provider-error"))
					return
				}
				if scenario == "code rejected" {
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
					return
				}
				body := map[string]any{"access_token": "provider-access-token", "token_type": "Bearer", "expires_in": 3600, "id_token": raw}
				if scenario == "missing id token" {
					delete(body, "id_token")
				}
				_ = json.NewEncoder(w).Encode(body)
			})
			provider := NewGoogleProvider("test-client", "test-secret", "http://127.0.0.1:8080/auth/google/callback")
			provider.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				return response.Result(), nil
			})
			provider.verifier = oidc.NewVerifier("https://accounts.google.com", &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}, &oidc.Config{ClientID: "test-client", SupportedSigningAlgs: []string{"RS256"}})
			identity, err := provider.Verify(context.Background(), "test-code", "test-nonce", "test-verifier")
			if scenario == "valid" {
				if err != nil || identity.Subject != "123456789" || !identity.EmailVerified {
					t.Fatalf("verification failed: %v", err)
				}
			} else if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("invalid identity/provider error accepted or leaked: %v", err)
			}
			if scenario == "provider unavailable" && !errors.Is(err, ErrProvider) {
				t.Fatal(err)
			}
		})
	}
}

func TestGoogleAuthorizationParameters(t *testing.T) {
	provider := NewGoogleProvider("test-client", "test-secret", "http://127.0.0.1:8080/auth/google/callback")
	address := provider.AuthorizationURL("state", "nonce", "verifier")
	parsed, _ := url.Parse(address)
	query := parsed.Query()
	if parsed.Host != "accounts.google.com" || query.Get("response_type") != "code" || query.Get("code_challenge_method") != "S256" || query.Get("nonce") != "nonce" || query.Get("state") != "state" || strings.Contains(address, "test-secret") {
		t.Fatal("unsafe authorization URL")
	}
}
