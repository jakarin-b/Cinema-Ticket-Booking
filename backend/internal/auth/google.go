package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/cinema-ticket-booking/backend/internal/config"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/idtoken"
)

type OAuthState struct {
	Nonce        string    `json:"nonce"`
	CodeVerifier string    `json:"code_verifier"`
	ReturnTo     string    `json:"return_to"`
	CreatedAt    time.Time `json:"created_at"`
}

type GoogleOAuth struct {
	redis           *redis.Client
	cfg             config.Config
	oauth           *oauth2.Config
	storeState      func(context.Context, string, []byte, time.Duration) error
	consumeState    func(context.Context, string) ([]byte, error)
	exchangeCode    func(context.Context, string, string) (string, error)
	validateIDToken func(context.Context, string, string) (*idtoken.Payload, error)
}

func NewGoogleOAuth(redisClient *redis.Client, cfg config.Config) *GoogleOAuth {
	g := &GoogleOAuth{redis: redisClient, cfg: cfg, oauth: &oauth2.Config{ClientID: cfg.GoogleClientID, ClientSecret: cfg.GoogleClientSecret, RedirectURL: cfg.GoogleRedirectURL, Scopes: []string{"openid", "email", "profile"}, Endpoint: google.Endpoint}}
	g.storeState = func(ctx context.Context, key string, value []byte, ttl time.Duration) error {
		return redisClient.Set(ctx, key, value, ttl).Err()
	}
	g.consumeState = func(ctx context.Context, key string) ([]byte, error) {
		return redisClient.GetDel(ctx, key).Bytes()
	}
	g.exchangeCode = func(ctx context.Context, code, verifier string) (string, error) {
		token, err := g.oauth.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
		if err != nil {
			return "", err
		}
		rawID, ok := token.Extra("id_token").(string)
		if !ok || rawID == "" {
			return "", ErrUnauthenticated
		}
		return rawID, nil
	}
	g.validateIDToken = idtoken.Validate
	return g
}

func (g *GoogleOAuth) Available() bool { return g != nil && g.cfg.GoogleConfigured() }
func (g *GoogleOAuth) Start(ctx context.Context, returnTo string) (authURL, state string, err error) {
	if !g.Available() {
		return "", "", ErrProviderUnavailable
	}
	if !validReturnTo(returnTo) {
		returnTo = "/"
	}
	state, err = randomToken(32)
	if err != nil {
		return "", "", err
	}
	nonce, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	verifier, err := randomToken(48)
	if err != nil {
		return "", "", err
	}
	record := OAuthState{Nonce: nonce, CodeVerifier: verifier, ReturnTo: returnTo, CreatedAt: time.Now().UTC()}
	raw, _ := json.Marshal(record)
	if err := g.storeState(ctx, "cinema:oauth-state:"+state, raw, g.cfg.OAuthStateTTL); err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	authURL = g.oauth.AuthCodeURL(state, oauth2.AccessTypeOnline, oauth2.SetAuthURLParam("nonce", nonce), oauth2.SetAuthURLParam("code_challenge", challenge), oauth2.SetAuthURLParam("code_challenge_method", "S256"), oauth2.SetAuthURLParam("prompt", "select_account"))
	return authURL, state, nil
}

func (g *GoogleOAuth) Callback(ctx context.Context, state, stateCookie, code string) (Claims, string, error) {
	if !g.Available() {
		return Claims{}, "", ErrProviderUnavailable
	}
	if state == "" || stateCookie == "" || state != stateCookie || code == "" {
		return Claims{}, "", ErrUnauthenticated
	}
	raw, err := g.consumeState(ctx, "cinema:oauth-state:"+state)
	if err != nil {
		return Claims{}, "", ErrUnauthenticated
	}
	var record OAuthState
	if json.Unmarshal(raw, &record) != nil {
		return Claims{}, "", ErrUnauthenticated
	}
	rawID, err := g.exchangeCode(ctx, code, record.CodeVerifier)
	if err != nil {
		return Claims{}, "", ErrUnauthenticated
	}
	payload, err := g.validateIDToken(ctx, rawID, g.cfg.GoogleClientID)
	if err != nil {
		return Claims{}, "", ErrUnauthenticated
	}
	nonce, _ := payload.Claims["nonce"].(string)
	if nonce != record.Nonce {
		return Claims{}, "", errors.New("oauth nonce mismatch")
	}
	email, _ := payload.Claims["email"].(string)
	name, _ := payload.Claims["name"].(string)
	picture, _ := payload.Claims["picture"].(string)
	verified := claimBool(payload.Claims["email_verified"])
	return Claims{Provider: "GOOGLE", Subject: payload.Subject, Email: email, EmailVerified: verified, DisplayName: name, AvatarURL: picture}, record.ReturnTo, nil
}

func validReturnTo(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.IsAbs() == false && strings.HasPrefix(parsed.Path, "/") && !strings.HasPrefix(value, "//")
}
