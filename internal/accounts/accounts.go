// Package accounts stores per-account mail settings for the local setup UI.
//
// Each account lives in its own directory below the base directory
// (default $XDG_CONFIG_HOME/icloud-mcp/accounts, else
// ~/.config/icloud-mcp/accounts):
//
//	<id>/account.json  settings without secrets (mode 0600)
//	<id>/mail_address  the login address (mode 0600)
//	<id>/password      the app password (mode 0600), write-only for the UI
//
// The server never reads this store directly. The store only produces the
// environment for one icloud-mcp process per account, with file:// references
// to the credential files, which config.Load already validates.
package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ThomasCrouzet/icloud-mcp/internal/config"
)

const (
	accountFile  = "account.json"
	addressFile  = "mail_address"
	passwordFile = "password"
	maxSecret    = 4 << 10
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// MailPermissions are the capability tokens an account may grant.
var MailPermissions = []string{"mail.read", "mail.mutate", "mail.trash", "mail.send", "mail.archive"}

// ErrNotFound reports a missing account.
var ErrNotFound = errors.New("account not found")

// Account is the non-secret part of one mail account.
type Account struct {
	ID                string   `json:"id"`
	Label             string   `json:"label,omitempty"`
	Address           string   `json:"address"`
	IMAPServer        string   `json:"imapServer,omitempty"`
	SMTPServer        string   `json:"smtpServer,omitempty"`
	Permissions       []string `json:"permissions"`
	AllowedRecipients string   `json:"allowedRecipients,omitempty"`
	ReadOnly          bool     `json:"readOnly"`
	DefaultTZ         string   `json:"defaultTZ,omitempty"`
	// HasPassword is computed from the store and never persisted.
	HasPassword bool `json:"hasPassword"`
}

// Store manages account directories below Dir.
type Store struct {
	Dir string
}

// DefaultDir returns the default account directory.
func DefaultDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" && filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "icloud-mcp", "accounts"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve home directory")
	}
	return filepath.Join(home, ".config", "icloud-mcp", "accounts"), nil
}

// Validate checks an account with the same parsers the server uses at boot.
// It normalizes server names and the permission order in place.
func (a *Account) Validate() error {
	if !idPattern.MatchString(a.ID) {
		return fmt.Errorf("id must be 1-32 characters: a-z, 0-9 and '-', starting with a letter or digit")
	}
	a.Label = strings.TrimSpace(a.Label)
	if len(a.Label) > 80 || strings.ContainsAny(a.Label, "\r\n\x00") {
		return fmt.Errorf("label must be one line of at most 80 characters")
	}
	a.Address = strings.TrimSpace(a.Address)
	if len(a.Address) < 4 || len(a.Address) > 254 || strings.ContainsAny(a.Address, " \r\n\x00<>,") || strings.Count(a.Address, "@") != 1 {
		return fmt.Errorf("address must be a plain email address")
	}
	imap, err := config.ParseMailServer("IMAP server", a.IMAPServer, 993)
	if err != nil {
		return err
	}
	smtp, err := config.ParseMailServer("SMTP server", a.SMTPServer, 587)
	if err != nil {
		return err
	}
	a.IMAPServer, a.SMTPServer = "", ""
	if imap.Host != "" {
		a.IMAPServer = imap.Address()
	}
	if smtp.Host != "" {
		a.SMTPServer = smtp.Address()
		if smtp.Port == 465 {
			return fmt.Errorf("SMTP port 465 (implicit TLS) is not supported; use 587")
		}
	}

	granted := map[string]bool{}
	for _, p := range a.Permissions {
		granted[strings.TrimSpace(p)] = true
	}
	a.Permissions = nil
	for _, p := range MailPermissions {
		if granted[p] {
			a.Permissions = append(a.Permissions, p)
			delete(granted, p)
		}
	}
	if len(granted) != 0 {
		return fmt.Errorf("unknown permission")
	}
	if len(a.Permissions) == 0 || a.Permissions[0] != "mail.read" {
		return fmt.Errorf("mail.read is required")
	}
	if _, err := config.ParsePermissions(strings.Join(a.Permissions, ",")); err != nil {
		return err
	}
	send := contains(a.Permissions, "mail.send")
	if contains(a.Permissions, "mail.archive") && !send {
		return fmt.Errorf("mail.archive requires mail.send")
	}
	a.AllowedRecipients = strings.TrimSpace(a.AllowedRecipients)
	if send {
		if _, err := config.ParseRecipientPolicy(a.AllowedRecipients); err != nil {
			return fmt.Errorf("allowed recipients: %v", err)
		}
		if a.IMAPServer != "" && a.SMTPServer == "" {
			return fmt.Errorf("an SMTP server is required for sending when the IMAP server is not iCloud")
		}
	} else {
		a.AllowedRecipients = ""
	}
	a.DefaultTZ = strings.TrimSpace(a.DefaultTZ)
	if strings.ContainsAny(a.DefaultTZ, " \r\n\x00") || len(a.DefaultTZ) > 64 {
		return fmt.Errorf("invalid time zone")
	}
	return nil
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func (s *Store) accountDir(id string) (string, error) {
	if !idPattern.MatchString(id) {
		return "", ErrNotFound
	}
	return filepath.Join(s.Dir, id), nil
}

// List returns all valid accounts sorted by id.
func (s *Store) List() ([]Account, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Account{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read account directory")
	}
	accounts := []Account{}
	for _, entry := range entries {
		if !entry.IsDir() || !idPattern.MatchString(entry.Name()) {
			continue
		}
		account, err := s.Get(entry.Name())
		if err != nil {
			continue
		}
		accounts = append(accounts, account)
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
	return accounts, nil
}

// Get loads one account.
func (s *Store) Get(id string) (Account, error) {
	dir, err := s.accountDir(id)
	if err != nil {
		return Account{}, err
	}
	data, err := readPrivateFile(filepath.Join(dir, accountFile))
	if err != nil {
		return Account{}, ErrNotFound
	}
	var account Account
	if err := json.Unmarshal(data, &account); err != nil || account.ID != id {
		return Account{}, ErrNotFound
	}
	_, err = os.Lstat(filepath.Join(dir, passwordFile))
	account.HasPassword = err == nil
	return account, nil
}

// Save validates and writes an account. An empty password keeps the stored
// one; a new account needs a password.
func (s *Store) Save(account Account, password string) (Account, error) {
	if err := account.Validate(); err != nil {
		return Account{}, err
	}
	if strings.ContainsAny(password, "\r\n\x00") || len(password) > maxSecret {
		return Account{}, fmt.Errorf("password must be one line")
	}
	if password != "" && len(password) < 8 {
		return Account{}, fmt.Errorf("password must be at least 8 characters")
	}
	dir, err := s.accountDir(account.ID)
	if err != nil {
		return Account{}, err
	}
	_, statErr := os.Lstat(filepath.Join(dir, passwordFile))
	if password == "" && statErr != nil {
		return Account{}, fmt.Errorf("password is required for a new account")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Account{}, fmt.Errorf("cannot create account directory")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return Account{}, fmt.Errorf("cannot secure account directory")
	}
	account.HasPassword = false
	data, err := json.MarshalIndent(account, "", "  ")
	if err != nil {
		return Account{}, err
	}
	if err := writePrivateFile(filepath.Join(dir, accountFile), append(data, '\n')); err != nil {
		return Account{}, err
	}
	if err := writePrivateFile(filepath.Join(dir, addressFile), []byte(account.Address)); err != nil {
		return Account{}, err
	}
	if password != "" {
		if err := writePrivateFile(filepath.Join(dir, passwordFile), []byte(password)); err != nil {
			return Account{}, err
		}
	}
	return s.Get(account.ID)
}

// Delete removes an account directory and its credential files.
func (s *Store) Delete(id string) error {
	dir, err := s.accountDir(id)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(dir); err != nil {
		return ErrNotFound
	}
	for _, name := range []string{passwordFile, addressFile, accountFile} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cannot delete account files")
		}
	}
	if err := os.Remove(dir); err != nil {
		return fmt.Errorf("account directory is not empty; remove it by hand")
	}
	return nil
}

// Env returns the environment for one icloud-mcp process serving this
// account. Credentials are file:// references, never values.
func (s *Store) Env(account Account) (map[string]string, error) {
	dir, err := s.accountDir(account.ID)
	if err != nil {
		return nil, err
	}
	env := map[string]string{
		"ICLOUD_MCP_ENABLE_MAIL": "true",
		"ICLOUD_MCP_PERMISSIONS": strings.Join(account.Permissions, ","),
		"ICLOUD_MCP_READ_ONLY":   fmt.Sprintf("%t", account.ReadOnly),
		"ICLOUD_MAIL_ADDRESS":    "file://" + filepath.Join(dir, addressFile),
		"ICLOUD_MAIL_PASSWORD":   "file://" + filepath.Join(dir, passwordFile),
	}
	if account.IMAPServer != "" {
		env["ICLOUD_MCP_IMAP_SERVER"] = account.IMAPServer
	}
	if account.SMTPServer != "" {
		env["ICLOUD_MCP_SMTP_SERVER"] = account.SMTPServer
	}
	if account.AllowedRecipients != "" {
		env["ICLOUD_MCP_SMTP_ALLOWED_RECIPIENTS"] = account.AllowedRecipients
	}
	if account.DefaultTZ != "" {
		env["ICLOUD_MCP_DEFAULT_TZ"] = account.DefaultTZ
	}
	return env, nil
}

// ServerName is the suggested MCP server name for an account.
func ServerName(account Account) string {
	return "mail-" + account.ID
}

// ClaudeCommand returns a `claude mcp add-json` command line that registers
// the account as a user-scoped MCP server. The JSON contains no secrets.
func (s *Store) ClaudeCommand(account Account, binary string) (string, error) {
	env, err := s.Env(account)
	if err != nil {
		return "", err
	}
	spec := map[string]any{"type": "stdio", "command": binary, "args": []string{}, "env": env}
	data, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("claude mcp add-json --scope user %s %s", ServerName(account), shellQuote(string(data))), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func writePrivateFile(path string, data []byte) error {
	tmp := path + ".tmp"
	_ = os.Remove(tmp)
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("cannot write account file")
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("cannot write account file")
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("cannot write account file")
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("cannot write account file")
	}
	return nil
}

func readPrivateFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return nil, ErrNotFound
	}
	return os.ReadFile(path)
}
