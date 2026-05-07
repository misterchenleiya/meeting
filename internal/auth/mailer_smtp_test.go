package auth

import (
	"net/smtp"
	"testing"
)

func TestSMTPMailerTLSModeDefaultsToStartTLS(t *testing.T) {
	mailer, err := NewSMTPMailer(nil, validSMTPMailerConfig())
	if err != nil {
		t.Fatalf("NewSMTPMailer() error = %v", err)
	}
	if mailer.effectiveTLSMode() != SMTPTLSModeStartTLS {
		t.Fatalf("effective tls mode = %q, want %q", mailer.effectiveTLSMode(), SMTPTLSModeStartTLS)
	}
}

func TestSMTPMailerTLSModeSupportsImplicit(t *testing.T) {
	config := validSMTPMailerConfig()
	config.SMTPPort = 465
	config.SMTPTLSMode = SMTPTLSModeImplicit

	mailer, err := NewSMTPMailer(nil, config)
	if err != nil {
		t.Fatalf("NewSMTPMailer() error = %v", err)
	}
	if mailer.effectiveTLSMode() != SMTPTLSModeImplicit {
		t.Fatalf("effective tls mode = %q, want %q", mailer.effectiveTLSMode(), SMTPTLSModeImplicit)
	}
}

func TestSMTPMailerTLSModeAutoUsesImplicitOnlyForPort465(t *testing.T) {
	config := validSMTPMailerConfig()
	config.SMTPPort = 465
	config.SMTPTLSMode = SMTPTLSModeAuto

	mailer, err := NewSMTPMailer(nil, config)
	if err != nil {
		t.Fatalf("NewSMTPMailer() error = %v", err)
	}
	if mailer.effectiveTLSMode() != SMTPTLSModeImplicit {
		t.Fatalf("effective tls mode = %q, want %q", mailer.effectiveTLSMode(), SMTPTLSModeImplicit)
	}

	config.SMTPPort = 80
	mailer, err = NewSMTPMailer(nil, config)
	if err != nil {
		t.Fatalf("NewSMTPMailer() error = %v", err)
	}
	if mailer.effectiveTLSMode() != SMTPTLSModeStartTLS {
		t.Fatalf("effective tls mode = %q, want %q", mailer.effectiveTLSMode(), SMTPTLSModeStartTLS)
	}
}

func TestSMTPMailerRejectsUnsupportedTLSMode(t *testing.T) {
	config := validSMTPMailerConfig()
	config.SMTPTLSMode = "ssl"

	if _, err := NewSMTPMailer(nil, config); err == nil {
		t.Fatal("NewSMTPMailer() error = nil, want unsupported tls mode error")
	}
}

func TestImplicitTLSPlainAuthDoesNotRequireSMTPStartTLSFlag(t *testing.T) {
	auth := &implicitTLSPlainAuth{
		username: "user@example.com",
		password: "secret",
	}

	protocol, response, err := auth.Start(&smtp.ServerInfo{
		Name: "smtp.example.com",
		TLS:  false,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if protocol != "PLAIN" {
		t.Fatalf("protocol = %q, want PLAIN", protocol)
	}
	if string(response) != "\x00user@example.com\x00secret" {
		t.Fatalf("response = %q, want smtp plain auth response", string(response))
	}
}

func validSMTPMailerConfig() MailerConfig {
	return MailerConfig{
		SMTPHost:        "smtp.example.com",
		SMTPPort:        587,
		SMTPFromAddress: "no-reply@example.com",
		SMTPRequireTLS:  true,
	}
}
