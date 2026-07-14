package auth

import (
	"context"
	"encoding/json"
	"fmt"

	firebase "firebase.google.com/go/v4"
	firebaseauth "firebase.google.com/go/v4/auth"
	"github.com/cinema-ticket-booking/backend/internal/config"
	"google.golang.org/api/option"
)

type Firebase struct {
	client    *firebaseauth.Client
	available bool
}

func NewFirebase(ctx context.Context, cfg config.Config) (*Firebase, error) {
	if !cfg.FirebaseConfigured() {
		return &Firebase{}, nil
	}
	credentials, err := json.Marshal(map[string]string{"type": "service_account", "project_id": cfg.FirebaseProjectID, "client_email": cfg.FirebaseClientEmail, "private_key": cfg.FirebasePrivateKey, "token_uri": "https://oauth2.googleapis.com/token"})
	if err != nil {
		return nil, err
	}
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: cfg.FirebaseProjectID}, option.WithCredentialsJSON(credentials))
	if err != nil {
		return nil, err
	}
	client, err := app.Auth(ctx)
	if err != nil {
		return nil, err
	}
	return &Firebase{client: client, available: true}, nil
}

func (f *Firebase) Available() bool { return f != nil && f.available }
func (f *Firebase) Verify(ctx context.Context, idToken string) (Claims, error) {
	if !f.Available() {
		return Claims{}, ErrProviderUnavailable
	}
	token, err := f.client.VerifyIDToken(ctx, idToken)
	if err != nil {
		return Claims{}, err
	}
	email, _ := token.Claims["email"].(string)
	name, _ := token.Claims["name"].(string)
	picture, _ := token.Claims["picture"].(string)
	verified := claimBool(token.Claims["email_verified"])
	return Claims{Provider: "FIREBASE", Subject: token.UID, Email: email, EmailVerified: verified, DisplayName: name, AvatarURL: picture, FirebaseUID: token.UID}, nil
}

func claimBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true"
	default:
		_ = fmt.Sprint(x)
		return false
	}
}
