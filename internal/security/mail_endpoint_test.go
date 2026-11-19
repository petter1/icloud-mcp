package security

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"
)

func TestMailEndpointDefaults(t *testing.T) {
	if DefaultIMAPEndpoint.Address() != IMAPAddress {
		t.Fatalf("default IMAP address = %q", DefaultIMAPEndpoint.Address())
	}
	if DefaultSMTPEndpoint.Address() != SMTPAddress {
		t.Fatalf("default SMTP address = %q", DefaultSMTPEndpoint.Address())
	}
	if !DefaultIMAPEndpoint.IsICloud() || !DefaultSMTPEndpoint.IsICloud() {
		t.Fatal("default endpoints must report iCloud")
	}
	if (MailEndpoint{Host: "mail.example.org", Port: 993}).IsICloud() {
		t.Fatal("custom endpoint must not report iCloud")
	}
}

func TestEndpointDialersAllowOnlyTheConfiguredServer(t *testing.T) {
	imap := MailEndpoint{Host: "mail.example.org", Port: 993}
	smtp := MailEndpoint{Host: "mail.example.org", Port: 587}
	for _, tt := range []struct {
		name    string
		dialer  DialContextFunc
		address string
	}{
		{name: "IMAP iCloud", address: IMAPAddress},
		{name: "IMAP other host", address: "evil.example:993"},
		{name: "IMAP other port", address: "mail.example.org:143"},
		{name: "SMTP iCloud", address: SMTPAddress},
		{name: "SMTP other port", address: "mail.example.org:25"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			injected := func(context.Context, string, string) (net.Conn, error) {
				calls++
				return nil, nil
			}
			var dial DialContextFunc
			if tt.name[:4] == "IMAP" {
				dial = NewIMAPEndpointDialer(imap, injected)
			} else {
				dial = NewSMTPEndpointDialer(smtp, injected)
			}
			if _, err := dial(context.Background(), "tcp", tt.address); err == nil {
				t.Fatal("expected allowlist rejection")
			}
			if calls != 0 {
				t.Fatalf("injected dialer called %d times", calls)
			}
		})
	}
}

func TestEndpointDialersRejectEmptyEndpoint(t *testing.T) {
	injected := func(context.Context, string, string) (net.Conn, error) { return nil, nil }
	if _, err := NewIMAPEndpointDialer(MailEndpoint{}, injected)(context.Background(), "tcp", ":0"); err == nil {
		t.Fatal("empty IMAP endpoint must be rejected")
	}
	if _, err := NewSMTPEndpointDialer(MailEndpoint{}, injected)(context.Background(), "tcp", ":0"); err == nil {
		t.Fatal("empty SMTP endpoint must be rejected")
	}
}

func TestSMTPEndpointDialerPassesConfiguredAddress(t *testing.T) {
	endpoint := MailEndpoint{Host: "mail.example.org", Port: 465}
	var got string
	dial := NewSMTPEndpointDialer(endpoint, func(_ context.Context, _ string, address string) (net.Conn, error) {
		got = address
		client, server := net.Pipe()
		_ = server.Close()
		return client, nil
	})
	conn, err := dial(context.Background(), "tcp", "mail.example.org:465")
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if got != "mail.example.org:465" {
		t.Fatalf("dial target = %q", got)
	}
}

func TestIMAPEndpointDialerVerifiesConfiguredHost(t *testing.T) {
	endpoint := MailEndpoint{Host: "mail.example.org", Port: 993}
	for _, tt := range []struct {
		name     string
		certHost string
		wantErr  bool
	}{
		{name: "matching certificate", certHost: "mail.example.org"},
		{name: "iCloud certificate on custom host", certHost: "imap.mail.me.com", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			certificate, roots := testHostCertificate(t, tt.certHost)
			client, server := net.Pipe()
			go func() {
				_ = tls.Server(server, &tls.Config{
					MinVersion:   tls.VersionTLS12,
					Certificates: []tls.Certificate{certificate},
				}).Handshake()
				_ = server.Close()
			}()
			dialer := newIMAPDialer(endpoint, func(context.Context, string, string) (net.Conn, error) {
				return client, nil
			}, &tls.Config{RootCAs: roots})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, err := dialer(ctx, "tcp", endpoint.Address())
			if tt.wantErr {
				if err == nil {
					_ = conn.Close()
					t.Fatal("expected certificate verification failure")
				}
				if stage, reason := IMAPFailureDetails(err); stage != "tls" || reason != "certificate_verification" {
					t.Fatalf("failure = %s/%s", stage, reason)
				}
				return
			}
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			state := conn.(*tls.Conn).ConnectionState()
			_ = conn.Close()
			if state.ServerName != endpoint.Host {
				t.Fatalf("ServerName = %q", state.ServerName)
			}
		})
	}
}

func testHostCertificate(t *testing.T, host string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: host},
		DNSNames:              []string{host},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: privateKey}, roots
}
