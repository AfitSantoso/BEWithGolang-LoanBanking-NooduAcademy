package googleauth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type GoogleProvider struct {
	config   oauth2.Config
	verifier *oidc.IDTokenVerifier
	client   *http.Client
}

func NewGoogleProvider(clientID, clientSecret, redirectURL string) *GoogleProvider {
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	keySet := oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), client), "https://www.googleapis.com/oauth2/v3/certs")
	return &GoogleProvider{
		config: oauth2.Config{ClientID: clientID, ClientSecret: clientSecret, RedirectURL: redirectURL, Scopes: []string{oidc.ScopeOpenID, "email", "profile"}, Endpoint: oauth2.Endpoint{
			AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", AuthStyle: oauth2.AuthStyleInParams,
		}},
		verifier: oidc.NewVerifier("https://accounts.google.com", keySet, &oidc.Config{ClientID: clientID, SupportedSigningAlgs: []string{"RS256"}}), client: client,
	}
}

func (provider *GoogleProvider) AuthorizationURL(state, nonce, verifier string) string {
	return provider.config.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("prompt", "select_account"))
}

func (provider *GoogleProvider) Verify(ctx context.Context, code, nonce, verifier string) (Identity, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, provider.client)
	token, err := provider.config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		var rejected *oauth2.RetrieveError
		if errors.As(err, &rejected) && rejected.Response.StatusCode >= 400 && rejected.Response.StatusCode < 500 {
			return Identity{}, ErrInvalidIdentity
		}
		return Identity{}, ErrProvider
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || len(raw) > 32768 {
		return Identity{}, ErrInvalidIdentity
	}
	idToken, err := provider.verifier.Verify(ctx, raw)
	if err != nil {
		return Identity{}, ErrInvalidIdentity
	}
	var claims struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
		Nonce    string `json:"nonce"`
		HD       string `json:"hd"`
		AZP      string `json:"azp"`
	}
	if idToken.Claims(&claims) != nil || subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(nonce)) != 1 {
		return Identity{}, ErrInvalidIdentity
	}
	if (claims.AZP != "" && claims.AZP != provider.config.ClientID) || (len(idToken.Audience) > 1 && claims.AZP != provider.config.ClientID) {
		return Identity{}, ErrInvalidIdentity
	}
	if idToken.AccessTokenHash != "" && idToken.VerifyAccessToken(token.AccessToken) != nil {
		return Identity{}, ErrInvalidIdentity
	}
	return Identity{Subject: idToken.Subject, Email: claims.Email, EmailVerified: claims.Verified, HostedDomain: claims.HD}, nil
}
