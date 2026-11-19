package config

import (
	"strings"
	"testing"

	"github.com/ThomasCrouzet/icloud-mcp/internal/security"
)

func TestParseMailServer(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  security.MailEndpoint
	}{
		{value: "", want: security.MailEndpoint{}},
		{value: "  ", want: security.MailEndpoint{}},
		{value: "mail.example.org", want: security.MailEndpoint{Host: "mail.example.org", Port: 993}},
		{value: "Mail.Example.ORG.", want: security.MailEndpoint{Host: "mail.example.org", Port: 993}},
		{value: "mail.example.org:1993", want: security.MailEndpoint{Host: "mail.example.org", Port: 1993}},
		{value: " imap-1.mail.example.org:993 ", want: security.MailEndpoint{Host: "imap-1.mail.example.org", Port: 993}},
	} {
		got, err := ParseMailServer(imapServerEnvVar, tt.value, 993)
		if err != nil {
			t.Fatalf("ParseMailServer(%q): %v", tt.value, err)
		}
		if got != tt.want {
			t.Errorf("ParseMailServer(%q) = %+v, want %+v", tt.value, got, tt.want)
		}
	}
}

func TestParseMailServerRejects(t *testing.T) {
	for _, value := range []string{
		"localhost",
		"192.168.1.10",
		"192.168.1.10:993",
		"[::1]:993",
		"::1",
		"mail.example.org:",
		"mail.example.org:0",
		"mail.example.org:65536",
		"mail.example.org:099",
		"mail.example.org:abc",
		"*.example.org",
		"-mail.example.org",
		"mail_server.example.org",
		"mail..example.org",
		"mail.example.org/path",
		"user@mail.example.org",
		"https://mail.example.org",
		"mail.example.123",
	} {
		_, err := ParseMailServer(imapServerEnvVar, value, 993)
		if err == nil {
			t.Errorf("ParseMailServer(%q) accepted an invalid server", value)
			continue
		}
		if !strings.Contains(err.Error(), imapServerEnvVar) {
			t.Errorf("error for %q does not name the variable: %v", value, err)
		}
	}
}

func setMailOnlyEnv(t *testing.T, permissions string) {
	t.Helper()
	clearEnv(t)
	t.Setenv("ICLOUD_MCP_ENABLE_MAIL", "true")
	t.Setenv("ICLOUD_MAIL_ADDRESS", "me@example.org")
	t.Setenv("ICLOUD_MAIL_PASSWORD", "separate-mail-password")
	t.Setenv("ICLOUD_MCP_PERMISSIONS", permissions)
}

func TestLoad_DefaultMailServersAreICloud(t *testing.T) {
	setMailOnlyEnv(t, "mail.read")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MailIMAPEndpoint() != security.DefaultIMAPEndpoint || cfg.MailSMTPEndpoint() != security.DefaultSMTPEndpoint {
		t.Fatalf("default endpoints = %+v / %+v", cfg.MailIMAPEndpoint(), cfg.MailSMTPEndpoint())
	}
}

func TestLoad_CustomMailServers(t *testing.T) {
	setMailOnlyEnv(t, "mail.read,mail.send")
	t.Setenv("ICLOUD_MCP_SMTP_ALLOWED_RECIPIENTS", "me@example.org")
	t.Setenv("ICLOUD_MCP_IMAP_SERVER", "mail.example.org")
	t.Setenv("ICLOUD_MCP_SMTP_SERVER", "mail.example.org")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.MailIMAPEndpoint().Address(); got != "mail.example.org:993" {
		t.Errorf("IMAP = %s", got)
	}
	if got := cfg.MailSMTPEndpoint().Address(); got != "mail.example.org:587" {
		t.Errorf("SMTP = %s", got)
	}
}

func TestLoad_CustomMailServerErrors(t *testing.T) {
	for _, tt := range []struct {
		name        string
		permissions string
		env         map[string]string
		wantErr     string
	}{
		{
			name:        "server without Mail",
			permissions: "calendar.read",
			env: map[string]string{
				"ICLOUD_EMAIL":           "user@example.com",
				"ICLOUD_PASSWORD":        "app-specific-password",
				"ICLOUD_MCP_IMAP_SERVER": "mail.example.org",
			},
			wantErr: "require Mail",
		},
		{
			name:        "implicit TLS SMTP port",
			permissions: "mail.read",
			env:         map[string]string{"ICLOUD_MCP_SMTP_SERVER": "mail.example.org:465"},
			wantErr:     "465",
		},
		{
			name:        "custom IMAP with send but iCloud SMTP",
			permissions: "mail.read,mail.send",
			env: map[string]string{
				"ICLOUD_MCP_IMAP_SERVER":             "mail.example.org",
				"ICLOUD_MCP_SMTP_ALLOWED_RECIPIENTS": "me@example.org",
			},
			wantErr: "ICLOUD_MCP_SMTP_SERVER is required",
		},
		{
			name:        "invalid host",
			permissions: "mail.read",
			env:         map[string]string{"ICLOUD_MCP_IMAP_SERVER": "10.0.0.1"},
			wantErr:     "ICLOUD_MCP_IMAP_SERVER",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setMailOnlyEnv(t, tt.permissions)
			if !strings.HasPrefix(tt.permissions, "mail") {
				t.Setenv("ICLOUD_MCP_ENABLE_MAIL", "")
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want containing %q", err, tt.wantErr)
			}
			if strings.Contains(err.Error(), "separate-mail-password") || strings.Contains(err.Error(), "me@example.org") {
				t.Fatalf("error leaks a credential: %v", err)
			}
		})
	}
}
