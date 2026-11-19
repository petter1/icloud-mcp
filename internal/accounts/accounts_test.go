package accounts

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validAccount() Account {
	return Account{
		ID:          "work",
		Label:       "Work",
		Address:     "me@example.org",
		IMAPServer:  "Mail.Example.org",
		SMTPServer:  "mail.example.org",
		Permissions: []string{"mail.mutate", "mail.read"},
		ReadOnly:    true,
	}
}

func TestValidateNormalizes(t *testing.T) {
	a := validAccount()
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if a.IMAPServer != "mail.example.org:993" || a.SMTPServer != "mail.example.org:587" {
		t.Fatalf("servers = %q / %q", a.IMAPServer, a.SMTPServer)
	}
	if strings.Join(a.Permissions, ",") != "mail.read,mail.mutate" {
		t.Fatalf("permissions = %v", a.Permissions)
	}
}

func TestValidateRejects(t *testing.T) {
	for name, mutate := range map[string]func(*Account){
		"bad id":               func(a *Account) { a.ID = "../etc" },
		"upper id":             func(a *Account) { a.ID = "Work" },
		"no read":              func(a *Account) { a.Permissions = []string{"mail.mutate"} },
		"unknown permission":   func(a *Account) { a.Permissions = []string{"mail.read", "calendar.read"} },
		"archive without send": func(a *Account) { a.Permissions = []string{"mail.read", "mail.archive"} },
		"send without policy":  func(a *Account) { a.Permissions = []string{"mail.read", "mail.send"} },
		"send without smtp": func(a *Account) {
			a.Permissions = []string{"mail.read", "mail.send"}
			a.AllowedRecipients = "me@example.org"
			a.SMTPServer = ""
		},
		"ip server":   func(a *Account) { a.IMAPServer = "10.0.0.1" },
		"smtp 465":    func(a *Account) { a.SMTPServer = "mail.example.org:465" },
		"bad address": func(a *Account) { a.Address = "not an address" },
		"label lines": func(a *Account) { a.Label = "a\nb" },
	} {
		t.Run(name, func(t *testing.T) {
			a := validAccount()
			mutate(&a)
			if err := a.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestStoreRoundTripAndPrivateFiles(t *testing.T) {
	store := &Store{Dir: filepath.Join(t.TempDir(), "accounts")}
	if _, err := store.Save(validAccount(), ""); err == nil {
		t.Fatal("new account without password must fail")
	}
	saved, err := store.Save(validAccount(), "app-password-1")
	if err != nil {
		t.Fatal(err)
	}
	if !saved.HasPassword {
		t.Fatal("HasPassword not set")
	}
	dir := filepath.Join(store.Dir, "work")
	for _, name := range []string{"", accountFile, addressFile, passwordFile} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0o600)
		if name == "" {
			want = 0o700
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode = %v, want %v", name, info.Mode().Perm(), want)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, accountFile))
	if strings.Contains(string(data), "app-password-1") || strings.Contains(string(data), "hasPassword\": true") {
		t.Fatal("account.json contains a secret or computed field")
	}

	// Empty password on update keeps the stored one.
	update := validAccount()
	update.Label = "Renamed"
	if _, err := store.Save(update, ""); err != nil {
		t.Fatal(err)
	}
	pw, _ := os.ReadFile(filepath.Join(dir, passwordFile))
	if string(pw) != "app-password-1" {
		t.Fatal("password changed on update without a new password")
	}

	list, err := store.List()
	if err != nil || len(list) != 1 || list[0].Label != "Renamed" {
		t.Fatalf("List = %+v, %v", list, err)
	}

	if err := store.Delete("work"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("account directory still exists")
	}
	if err := store.Delete("work"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete = %v", err)
	}
}

func TestListIgnoresForeignEntries(t *testing.T) {
	store := &Store{Dir: t.TempDir()}
	_ = os.WriteFile(filepath.Join(store.Dir, "stray.txt"), []byte("x"), 0o600)
	_ = os.Mkdir(filepath.Join(store.Dir, "broken"), 0o700)
	_ = os.WriteFile(filepath.Join(store.Dir, "broken", accountFile), []byte("{"), 0o600)
	list, err := store.List()
	if err != nil || len(list) != 0 {
		t.Fatalf("List = %+v, %v", list, err)
	}
}

func TestEnvAndClaudeCommandUseFileReferences(t *testing.T) {
	store := &Store{Dir: "/home/u/.config/icloud-mcp/accounts"}
	a := validAccount()
	a.Permissions = []string{"mail.read", "mail.send"}
	a.AllowedRecipients = "me@example.org"
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	env, err := store.Env(a)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"ICLOUD_MCP_ENABLE_MAIL":             "true",
		"ICLOUD_MCP_PERMISSIONS":             "mail.read,mail.send",
		"ICLOUD_MCP_READ_ONLY":               "true",
		"ICLOUD_MAIL_ADDRESS":                "file:///home/u/.config/icloud-mcp/accounts/work/mail_address",
		"ICLOUD_MAIL_PASSWORD":               "file:///home/u/.config/icloud-mcp/accounts/work/password",
		"ICLOUD_MCP_IMAP_SERVER":             "mail.example.org:993",
		"ICLOUD_MCP_SMTP_SERVER":             "mail.example.org:587",
		"ICLOUD_MCP_SMTP_ALLOWED_RECIPIENTS": "me@example.org",
	}
	if len(env) != len(want) {
		t.Fatalf("env = %v", env)
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
	cmd, err := store.ClaudeCommand(a, "/home/u/.local/bin/icloud-mcp")
	if err != nil {
		t.Fatal(err)
	}
	prefix := "claude mcp add-json --scope user mail-work '"
	if !strings.HasPrefix(cmd, prefix) || !strings.HasSuffix(cmd, "'") {
		t.Fatalf("command = %s", cmd)
	}
	var spec struct {
		Type    string            `json:"type"`
		Command string            `json:"command"`
		Env     map[string]string `json:"env"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(cmd, prefix), "'")), &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Type != "stdio" || spec.Command != "/home/u/.local/bin/icloud-mcp" || spec.Env["ICLOUD_MCP_IMAP_SERVER"] != "mail.example.org:993" {
		t.Fatalf("spec = %+v", spec)
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote(`a'b`); got != `'a'\''b'` {
		t.Fatalf("shellQuote = %s", got)
	}
}

func TestDefaultDirUsesXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	dir, err := DefaultDir()
	if err != nil || dir != "/xdg/icloud-mcp/accounts" {
		t.Fatalf("DefaultDir = %q, %v", dir, err)
	}
	t.Setenv("XDG_CONFIG_HOME", "relative")
	t.Setenv("HOME", "/home/u")
	dir, err = DefaultDir()
	if err != nil || dir != "/home/u/.config/icloud-mcp/accounts" {
		t.Fatalf("DefaultDir fallback = %q, %v", dir, err)
	}
}
