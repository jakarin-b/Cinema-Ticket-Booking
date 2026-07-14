package auth

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cinema-ticket-booking/backend/internal/config"
	"github.com/redis/go-redis/v9"
	"google.golang.org/api/idtoken"
)

func TestValidReturnTo(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{{"", true}, {"/checkout/123?from=login", true}, {"//evil.example", false}, {"https://evil.example", false}, {"javascript:alert(1)", false}}
	for _, test := range tests {
		if got := validReturnTo(test.value); got != test.want {
			t.Errorf("validReturnTo(%q)=%v want %v", test.value, got, test.want)
		}
	}
}

func TestClaimBool(t *testing.T) {
	if !claimBool(true) || !claimBool("true") || claimBool(false) || claimBool("false") {
		t.Fatal("claimBool did not normalize supported values")
	}
}

func TestGoogleCallbackConsumesStateAndValidatesNonce(t *testing.T) {
	record := OAuthState{Nonce: "expected-nonce", CodeVerifier: "pkce-verifier", ReturnTo: "/bookings", CreatedAt: time.Now().UTC()}
	raw, _ := json.Marshal(record)
	consumed := false
	g := &GoogleOAuth{
		cfg: config.Config{GoogleClientID: "client-id", GoogleClientSecret: "client-secret", GoogleRedirectURL: "http://localhost/callback"},
		consumeState: func(_ context.Context, key string) ([]byte, error) {
			if key != "cinema:oauth-state:valid-state" || consumed {
				return nil, redis.Nil
			}
			consumed = true
			return raw, nil
		},
		exchangeCode: func(_ context.Context, code, verifier string) (string, error) {
			if code != "one-time-code" || verifier != record.CodeVerifier {
				return "", errors.New("unexpected exchange input")
			}
			return "signed-id-token", nil
		},
		validateIDToken: func(_ context.Context, token, audience string) (*idtoken.Payload, error) {
			if token != "signed-id-token" || audience != "client-id" {
				return nil, errors.New("unexpected token verification input")
			}
			return &idtoken.Payload{Issuer: "https://accounts.google.com", Audience: audience, Subject: "google-subject", Expires: time.Now().Add(time.Hour).Unix(), Claims: map[string]any{"nonce": record.Nonce, "email": "User@Example.com", "email_verified": true, "name": "User"}}, nil
		},
	}

	claims, returnTo, err := g.Callback(context.Background(), "valid-state", "valid-state", "one-time-code")
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "google-subject" || !claims.EmailVerified || returnTo != "/bookings" {
		t.Fatalf("unexpected callback result: %#v %q", claims, returnTo)
	}
	if _, _, err := g.Callback(context.Background(), "valid-state", "valid-state", "one-time-code"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("expected replayed state to be rejected")
	}
}

func TestGoogleCallbackRejectsStateAndNonceMismatch(t *testing.T) {
	record := OAuthState{Nonce: "expected", CodeVerifier: "verifier"}
	raw, _ := json.Marshal(record)
	g := &GoogleOAuth{
		cfg:          config.Config{GoogleClientID: "client-id", GoogleClientSecret: "secret", GoogleRedirectURL: "http://localhost/callback"},
		consumeState: func(context.Context, string) ([]byte, error) { return raw, nil },
		exchangeCode: func(context.Context, string, string) (string, error) { return "token", nil },
		validateIDToken: func(context.Context, string, string) (*idtoken.Payload, error) {
			return &idtoken.Payload{Subject: "subject", Claims: map[string]any{"nonce": "wrong", "email": "user@example.com", "email_verified": true}}, nil
		},
	}
	if _, _, err := g.Callback(context.Background(), "state", "different", "code"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("expected state mismatch to be rejected")
	}
	if _, _, err := g.Callback(context.Background(), "state", "state", "code"); err == nil || err.Error() != "oauth nonce mismatch" {
		t.Fatalf("expected nonce mismatch, got %v", err)
	}
}
