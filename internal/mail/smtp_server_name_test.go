package mail

import (
	"context"
	"testing"
	"time"
)

func TestSMTPSessionFactoryVerifiesConfiguredServerName(t *testing.T) {
	for _, tt := range []struct {
		name       string
		serverName string
		wantErr    bool
	}{
		{name: "default iCloud name", serverName: ""},
		{name: "explicit iCloud name", serverName: smtpHost},
		{name: "custom name rejects iCloud certificate", serverName: "mail.example.org", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clientTLS, serverTLS := smtpTestTLSConfigs(t)
			clientConn, _, serverErr := startScriptedSMTP(serverTLS,
				"250-localhost\r\n250 AUTH PLAIN\r\n", "235 2.7.0 authenticated\r\n", true)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			factory := smtpSessionFactoryWithRoots(tt.serverName, clientTLS.RootCAs)
			session, err := factory(ctx, clientConn)
			if tt.wantErr {
				if err == nil {
					_ = session.Close()
					t.Fatal("expected TLS verification failure for a mismatched server name")
				}
				_ = clientConn.Close()
				if err := <-serverErr; err == nil {
					t.Fatal("server handshake should fail for a mismatched name")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := session.Auth("sender@icloud.com", "test-password"); err != nil {
				t.Fatal(err)
			}
			_ = session.Close()
			if err := <-serverErr; err != nil {
				t.Fatal(err)
			}
		})
	}
}
