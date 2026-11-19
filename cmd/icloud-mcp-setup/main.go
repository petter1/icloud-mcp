// Command icloud-mcp-setup is a small local web UI to manage mail accounts
// for icloud-mcp (one server process per account).
//
// Security model:
//   - listens on 127.0.0.1 only, on a random port unless -addr is given
//   - every request needs a random session token (printed once on start)
//   - the Host header must be the listen address (DNS-rebinding guard)
//   - state-changing requests must be same-origin JSON POST/DELETE
//   - passwords are write-only: the UI can set them but never reads them back
//   - the program never starts other processes and does not edit Claude's
//     configuration; it prints a `claude mcp add-json` command to copy
package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/ThomasCrouzet/icloud-mcp/internal/accounts"
)

//go:embed index.html
var indexHTML []byte

const (
	tokenCookie  = "icloud_mcp_setup"
	maxBodyBytes = 16 << 10
)

type server struct {
	store  *accounts.Store
	token  string
	host   string
	binary string
}

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "listen address (loopback only)")
	dir := flag.String("dir", "", "account directory (default ~/.config/icloud-mcp/accounts)")
	binary := flag.String("binary", "", "icloud-mcp binary path used in the generated command (default ~/.local/bin/icloud-mcp)")
	flag.Parse()

	if err := run(*addr, *dir, *binary, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "icloud-mcp-setup:", err)
		os.Exit(1)
	}
}

func run(addr, dir, binary string, stdout io.Writer) error {
	srv, listener, err := newServer(addr, dir, binary)
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Handler:           srv.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	link := url.URL{Scheme: "http", Host: srv.host, Path: "/", RawQuery: "token=" + srv.token}
	fmt.Fprintf(stdout, "icloud-mcp-setup: open %s\n", link.String())
	fmt.Fprintln(stdout, "Stop with Ctrl+C. The link works only for this run.")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}()
	if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func newServer(addr, dir, binary string) (*server, net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid -addr")
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() || ip.To4() == nil {
		return nil, nil, fmt.Errorf("-addr must be an IPv4 loopback address such as 127.0.0.1")
	}
	if dir == "" {
		dir, err = accounts.DefaultDir()
		if err != nil {
			return nil, nil, err
		}
	}
	if !filepath.IsAbs(dir) {
		return nil, nil, fmt.Errorf("-dir must be an absolute path")
	}
	if binary == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, fmt.Errorf("cannot resolve home directory; pass -binary")
		}
		binary = filepath.Join(home, ".local", "bin", "icloud-mcp")
	}
	token, err := randomToken()
	if err != nil {
		return nil, nil, err
	}
	listener, err := net.Listen("tcp4", addr)
	if err != nil {
		return nil, nil, err
	}
	return &server{
		store:  &accounts.Store{Dir: dir},
		token:  token,
		host:   listener.Addr().String(),
		binary: binary,
	}, listener, nil
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("cannot create session token")
	}
	return hex.EncodeToString(buf), nil
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /api/accounts", s.handleList)
	mux.HandleFunc("POST /api/accounts", s.handleSave)
	mux.HandleFunc("DELETE /api/accounts/{id}", s.handleDelete)
	return s.guard(mux)
}

// guard applies the host, token and origin checks to every request.
func (s *server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Cache-Control", "no-store")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; form-action 'none'; frame-ancestors 'none'; base-uri 'none'")

		if r.Host != s.host {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/" && r.URL.Query().Has("token") {
			if !s.validToken(r.URL.Query().Get("token")) {
				http.Error(w, "invalid token", http.StatusForbidden)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name: tokenCookie, Value: s.token, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteStrictMode,
			})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		cookie, err := r.Cookie(tokenCookie)
		if err != nil || !s.validToken(cookie.Value) {
			http.Error(w, "open the link printed in the terminal", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			if r.Header.Get("Origin") != s.origin() {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
			if !s.validToken(r.Header.Get("X-Setup-Token")) {
				http.Error(w, "missing request token", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// origin is the only accepted Origin for state-changing requests.
func (s *server) origin() string {
	return (&url.URL{Scheme: "http", Host: s.host}).String()
}

func (s *server) validToken(value string) bool {
	return len(value) == len(s.token) && subtle.ConstantTimeCompare([]byte(value), []byte(s.token)) == 1
}

func (s *server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := strings.Replace(string(indexHTML), "{{TOKEN}}", s.token, 1)
	_, _ = io.WriteString(w, page)
}

type accountView struct {
	accounts.Account
	ServerName string            `json:"serverName"`
	Env        map[string]string `json:"env"`
	Command    string            `json:"command"`
}

func (s *server) view(account accounts.Account) (accountView, error) {
	env, err := s.store.Env(account)
	if err != nil {
		return accountView{}, err
	}
	command, err := s.store.ClaudeCommand(account, s.binary)
	if err != nil {
		return accountView{}, err
	}
	return accountView{Account: account, ServerName: accounts.ServerName(account), Env: env, Command: command}, nil
}

func (s *server) handleList(w http.ResponseWriter, _ *http.Request) {
	list, err := s.store.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	views := make([]accountView, 0, len(list))
	for _, account := range list {
		view, err := s.view(account)
		if err != nil {
			continue
		}
		views = append(views, view)
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": views, "dir": s.store.Dir, "binary": s.binary})
}

type saveRequest struct {
	accounts.Account
	Password string `json:"password"`
}

func (s *server) handleSave(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, fmt.Errorf("JSON required"))
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	var request saveRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request"))
		return
	}
	saved, err := s.store.Save(request.Account, request.Password)
	request.Password = ""
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.view(saved)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Delete(r.PathValue("id")); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, accounts.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
