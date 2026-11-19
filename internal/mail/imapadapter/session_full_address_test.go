package imapadapter

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// scriptedLoginServer answers every LOGIN with reply and records commands.
func scriptedLoginServer(t *testing.T, reply string) (net.Conn, <-chan string, <-chan error) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	commands := make(chan string, 4)
	serverErr := make(chan error, 1)
	go func() {
		defer func() { _ = serverConn.Close() }()
		reader := bufio.NewReader(serverConn)
		if _, err := fmt.Fprint(serverConn, "* OK [CAPABILITY IMAP4rev1] ready\r\n"); err != nil {
			serverErr <- err
			return
		}
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				serverErr <- nil
				return
			}
			commands <- line
			tag := commandTag(line)
			switch {
			case strings.Contains(line, "LOGOUT"):
				_, err = fmt.Fprintf(serverConn, "* BYE closing\r\n%s OK logout\r\n", tag)
				serverErr <- err
				return
			case strings.Contains(line, "LOGIN"):
				if _, err := fmt.Fprint(serverConn, tag+reply); err != nil {
					serverErr <- err
					return
				}
			}
		}
	}()
	return clientConn, commands, serverErr
}

func TestFullAddressSessionLogsInWithFullAddressOnly(t *testing.T) {
	clientConn, commands, serverErr := scriptedLoginServer(t, " OK [CAPABILITY IMAP4rev1] authenticated\r\n")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	session, err := NewFullAddressSession(ctx, clientConn, "me@example.org", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	first := <-commands
	if !strings.Contains(first, `LOGIN "me@example.org"`) {
		t.Fatalf("first command = %s", first)
	}
	if logout := <-commands; !strings.Contains(logout, "LOGOUT") {
		t.Fatalf("expected LOGOUT after a single LOGIN, got %s", logout)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func TestFullAddressSessionDoesNotRetryAfterRejection(t *testing.T) {
	clientConn, commands, serverErr := scriptedLoginServer(t, " NO [AUTHENTICATIONFAILED] rejected\r\n")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := NewFullAddressSession(ctx, clientConn, "me@example.org", "test-password")
	if err == nil || !IsLoginRejected(err) {
		t.Fatalf("err = %v, want login rejection", err)
	}
	<-serverErr
	var logins int
	for {
		select {
		case line := <-commands:
			if strings.Contains(line, "LOGIN") {
				logins++
			}
			continue
		default:
		}
		break
	}
	if logins != 1 {
		t.Fatalf("LOGIN attempts = %d, want exactly 1", logins)
	}
}
