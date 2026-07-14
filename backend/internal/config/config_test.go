package config

import "testing"

func TestProductionRequiresSecureCookie(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("COOKIE_SECURE", "false")
	if _, err := Load(); err == nil {
		t.Fatal("expected production configuration to reject an insecure cookie")
	}
}

func TestAdminEmailsAreNormalized(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("ADMIN_EMAILS", "Admin@Example.com, reviewer@example.com")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IsAdmin(" admin@example.com ") {
		t.Fatal("expected normalized admin email to match")
	}
	if cfg.IsAdmin("user@example.com") {
		t.Fatal("unexpected admin role")
	}
}
