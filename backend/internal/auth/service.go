package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/cinema-ticket-booking/backend/internal/config"
	"github.com/cinema-ticket-booking/backend/internal/database"
	"github.com/cinema-ticket-booking/backend/internal/domain"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	ErrUnauthenticated     = errors.New("authentication required")
	ErrProviderUnavailable = errors.New("authentication provider is not configured")
	ErrUnverifiedEmail     = errors.New("provider email is not verified")
)

type Claims struct {
	Provider      string
	Subject       string
	Email         string
	EmailVerified bool
	DisplayName   string
	AvatarURL     string
	FirebaseUID   string
}

type Session struct {
	UserID    string    `json:"user_id"`
	CSRFToken string    `json:"csrf_token"`
	CreatedAt time.Time `json:"created_at"`
}

type Principal struct {
	User         domain.User
	Method       string
	CSRFToken    string
	SessionToken string
}

type FirebaseVerifier interface {
	Verify(context.Context, string) (Claims, error)
	Available() bool
}

type Service struct {
	store    *database.Store
	redis    *redis.Client
	cfg      config.Config
	firebase FirebaseVerifier
}

func NewService(store *database.Store, redisClient *redis.Client, cfg config.Config, firebase FirebaseVerifier) *Service {
	return &Service{store: store, redis: redisClient, cfg: cfg, firebase: firebase}
}

func (s *Service) VerifyFirebase(ctx context.Context, token string) (*Principal, error) {
	if s.firebase == nil || !s.firebase.Available() {
		return nil, ErrProviderUnavailable
	}
	claims, err := s.firebase.Verify(ctx, token)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	user, err := s.ResolveIdentity(ctx, claims)
	if err != nil {
		return nil, err
	}
	return &Principal{User: *user, Method: "firebase"}, nil
}

func (s *Service) ResolveIdentity(ctx context.Context, claims Claims) (*domain.User, error) {
	claims.Email = strings.ToLower(strings.TrimSpace(claims.Email))
	if !claims.EmailVerified || claims.Email == "" || claims.Provider == "" || claims.Subject == "" {
		return nil, ErrUnverifiedEmail
	}
	now := time.Now().UTC()
	role := domain.RoleUser
	if s.cfg.IsAdmin(claims.Email) {
		role = domain.RoleAdmin
	}
	result, err := s.store.WithTransaction(ctx, func(sc mongo.SessionContext) (any, error) {
		var identity domain.AuthIdentity
		err := s.store.DB.Collection("auth_identities").FindOne(sc, bson.M{"provider": claims.Provider, "subject": claims.Subject}).Decode(&identity)
		var user domain.User
		if err == nil {
			if err := s.store.DB.Collection("users").FindOne(sc, bson.M{"_id": identity.UserID}).Decode(&user); err != nil {
				return nil, err
			}
			update := bson.M{"email": claims.Email, "display_name": claims.DisplayName, "avatar_url": claims.AvatarURL, "role": role, "updated_at": now}
			if claims.FirebaseUID != "" {
				update["firebase_uid"] = claims.FirebaseUID
			}
			if _, err := s.store.DB.Collection("users").UpdateOne(sc, bson.M{"_id": user.ID}, bson.M{"$set": update}); err != nil {
				return nil, err
			}
			_, _ = s.store.DB.Collection("auth_identities").UpdateOne(sc, bson.M{"_id": identity.ID}, bson.M{"$set": bson.M{"verified_email": claims.Email, "last_login_at": now}})
			user.Email, user.DisplayName, user.AvatarURL, user.Role, user.UpdatedAt = claims.Email, claims.DisplayName, claims.AvatarURL, role, now
			if claims.FirebaseUID != "" {
				user.FirebaseUID = claims.FirebaseUID
			}
			return &user, nil
		}
		if err != mongo.ErrNoDocuments {
			return nil, err
		}
		err = s.store.DB.Collection("users").FindOne(sc, bson.M{"email": claims.Email}).Decode(&user)
		if err == mongo.ErrNoDocuments {
			user = domain.User{ID: primitive.NewObjectID(), Email: claims.Email, DisplayName: claims.DisplayName, AvatarURL: claims.AvatarURL, Role: role, CreatedAt: now, UpdatedAt: now}
			if claims.FirebaseUID != "" {
				user.FirebaseUID = claims.FirebaseUID
			}
			if _, err := s.store.DB.Collection("users").InsertOne(sc, user); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		} else {
			update := bson.M{"display_name": claims.DisplayName, "avatar_url": claims.AvatarURL, "role": role, "updated_at": now}
			if claims.FirebaseUID != "" {
				update["firebase_uid"] = claims.FirebaseUID
			}
			if _, err := s.store.DB.Collection("users").UpdateOne(sc, bson.M{"_id": user.ID}, bson.M{"$set": update}); err != nil {
				return nil, err
			}
			user.DisplayName, user.AvatarURL, user.Role, user.UpdatedAt = claims.DisplayName, claims.AvatarURL, role, now
			if claims.FirebaseUID != "" {
				user.FirebaseUID = claims.FirebaseUID
			}
		}
		identity = domain.AuthIdentity{ID: primitive.NewObjectID(), Provider: claims.Provider, Subject: claims.Subject, UserID: user.ID, VerifiedEmail: claims.Email, CreatedAt: now, LastLoginAt: now}
		if _, err := s.store.DB.Collection("auth_identities").InsertOne(sc, identity); err != nil {
			return nil, err
		}
		return &user, nil
	})
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			var identity domain.AuthIdentity
			if findErr := s.store.DB.Collection("auth_identities").FindOne(ctx, bson.M{"provider": claims.Provider, "subject": claims.Subject}).Decode(&identity); findErr == nil {
				var user domain.User
				if findErr = s.store.DB.Collection("users").FindOne(ctx, bson.M{"_id": identity.UserID}).Decode(&user); findErr == nil {
					return &user, nil
				}
			}
		}
		return nil, err
	}
	return result.(*domain.User), nil
}

func (s *Service) CreateSession(ctx context.Context, userID primitive.ObjectID) (token string, csrf string, err error) {
	token, err = randomToken(32)
	if err != nil {
		return "", "", err
	}
	csrf, err = randomToken(32)
	if err != nil {
		return "", "", err
	}
	session := Session{UserID: userID.Hex(), CSRFToken: csrf, CreatedAt: time.Now().UTC()}
	payload, _ := json.Marshal(session)
	if err := s.redis.Set(ctx, s.sessionKey(token), payload, s.cfg.SessionTTL).Err(); err != nil {
		return "", "", err
	}
	return token, csrf, nil
}

func (s *Service) Session(ctx context.Context, token string) (*Principal, error) {
	if token == "" {
		return nil, ErrUnauthenticated
	}
	raw, err := s.redis.Get(ctx, s.sessionKey(token)).Bytes()
	if err != nil {
		return nil, ErrUnauthenticated
	}
	var session Session
	if json.Unmarshal(raw, &session) != nil {
		return nil, ErrUnauthenticated
	}
	userID, err := primitive.ObjectIDFromHex(session.UserID)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	var user domain.User
	if err := s.store.DB.Collection("users").FindOne(ctx, bson.M{"_id": userID}).Decode(&user); err != nil {
		return nil, ErrUnauthenticated
	}
	role := domain.RoleUser
	if s.cfg.IsAdmin(user.Email) {
		role = domain.RoleAdmin
	}
	if user.Role != role {
		_, _ = s.store.DB.Collection("users").UpdateOne(ctx, bson.M{"_id": user.ID}, bson.M{"$set": bson.M{"role": role, "updated_at": time.Now().UTC()}})
		user.Role = role
	}
	return &Principal{User: user, Method: "google_oauth", CSRFToken: session.CSRFToken, SessionToken: token}, nil
}

func (s *Service) DeleteSession(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.redis.Del(ctx, s.sessionKey(token)).Err()
}
func (s *Service) CookieName() string             { return s.cfg.SessionCookieName }
func (s *Service) CookieSecure() bool             { return s.cfg.CookieSecure }
func (s *Service) SessionTTL() time.Duration      { return s.cfg.SessionTTL }
func (s *Service) sessionKey(token string) string { return "cinema:session:" + token }

func randomToken(bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
