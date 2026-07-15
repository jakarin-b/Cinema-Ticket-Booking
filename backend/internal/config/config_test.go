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

func TestSMTPDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("SMTP_HOST", "")
	t.Setenv("SMTP_PORT", "")
	t.Setenv("SMTP_USERNAME", "")
	t.Setenv("SMTP_PASSWORD", "")
	t.Setenv("SMTP_FROM", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SMTPHost != "localhost" || cfg.SMTPPort != 1025 || cfg.SMTPFrom != "Cinema Ticket Booking <no-reply@cinema.local>" {
		t.Fatalf("unexpected SMTP defaults: %#v", cfg)
	}
}

func TestSMTPConfigurationValidation(t *testing.T) {
	t.Run("invalid port", func(t *testing.T) {
		t.Setenv("SMTP_PORT", "0")
		if _, err := Load(); err == nil {
			t.Fatal("expected invalid SMTP port to fail")
		}
	})
	t.Run("invalid sender", func(t *testing.T) {
		t.Setenv("SMTP_FROM", "not-an-email")
		if _, err := Load(); err == nil {
			t.Fatal("expected invalid SMTP sender to fail")
		}
	})
	t.Run("incomplete credentials", func(t *testing.T) {
		t.Setenv("SMTP_USERNAME", "smtp-user")
		t.Setenv("SMTP_PASSWORD", "")
		if _, err := Load(); err == nil {
			t.Fatal("expected incomplete SMTP credentials to fail")
		}
	})
}
