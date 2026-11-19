package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/ThomasCrouzet/icloud-mcp/internal/security"
)

const (
	imapServerEnvVar = "ICLOUD_MCP_IMAP_SERVER"
	smtpServerEnvVar = "ICLOUD_MCP_SMTP_SERVER"
)

// ParseMailServer parses an operator-supplied mail server as host or
// host:port. An empty value returns the zero endpoint, which means the iCloud
// default. Only DNS host names are accepted: TLS must verify a name, so IP
// literals, single-label names and wildcards are rejected. The value is the
// complete network allowlist for that protocol in this process.
func ParseMailServer(envVar, value string, defaultPort int) (security.MailEndpoint, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return security.MailEndpoint{}, nil
	}
	host, port := value, defaultPort
	if strings.Contains(value, ":") {
		h, p, err := net.SplitHostPort(value)
		if err != nil || p == "" {
			return security.MailEndpoint{}, fmt.Errorf("invalid %s: expected host or host:port", envVar)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != p {
			return security.MailEndpoint{}, fmt.Errorf("invalid %s: port must be 1-65535", envVar)
		}
		host, port = h, n
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if !isDNSHostName(host) {
		return security.MailEndpoint{}, fmt.Errorf("invalid %s: host must be a DNS name with at least two labels (no IP address)", envVar)
	}
	return security.MailEndpoint{Host: host, Port: port}, nil
}

func isDNSHostName(host string) bool {
	if len(host) == 0 || len(host) > 253 || net.ParseIP(host) != nil {
		return false
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	tld := labels[len(labels)-1]
	if _, err := strconv.Atoi(tld); err == nil {
		return false
	}
	return true
}

// MailIMAPEndpoint returns the configured IMAP server or the iCloud default.
func (c *Config) MailIMAPEndpoint() security.MailEndpoint {
	if c.IMAPServer.Host == "" {
		return security.DefaultIMAPEndpoint
	}
	return c.IMAPServer
}

// MailSMTPEndpoint returns the configured SMTP server or the iCloud default.
func (c *Config) MailSMTPEndpoint() security.MailEndpoint {
	if c.SMTPServer.Host == "" {
		return security.DefaultSMTPEndpoint
	}
	return c.SMTPServer
}

// validateMailServers rejects server settings that cannot be used safely.
func (c *Config) validateMailServers() error {
	custom := c.IMAPServer.Host != "" || c.SMTPServer.Host != ""
	if !custom {
		return nil
	}
	if !c.requestsMail() {
		return fmt.Errorf("%s and %s require Mail to be enabled", imapServerEnvVar, smtpServerEnvVar)
	}
	if c.SMTPServer.Host != "" && c.SMTPServer.Port == 465 {
		return fmt.Errorf("invalid %s: port 465 (implicit TLS) is not supported; use a STARTTLS submission port such as 587", smtpServerEnvVar)
	}
	if c.IMAPServer.Host != "" && c.SMTPServer.Host == "" && c.requestedMailSend() {
		return fmt.Errorf("%s is required when %s is set and Mail send is requested", smtpServerEnvVar, imapServerEnvVar)
	}
	return nil
}

func (c *Config) requestedMailSend() bool {
	if c.Permissions.Configured() {
		return c.Permissions.RequestedMailSend()
	}
	return c.EnableMailSend
}
