package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func testServer(t *testing.T) *server {
	t.Helper()
	srv, listener, err := newServer("127.0.0.1:0", filepath.Join(t.TempDir(), "accounts"), "/opt/icloud-mcp")
	if err != nil {
		t.Fatal(err)
	}
	_ = listener.Close()
	return srv
}

func do(t *testing.T, srv *server, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	return rec
}

func authed(srv *server, method, path string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, "http://"+srv.host+path, body)
	req.Host = srv.host
	req.AddCookie(&http.Cookie{Name: tokenCookie, Value: srv.token})
	if method != http.MethodGet {
		req.Header.Set("Origin", "http://"+srv.host)
		req.Header.Set("X-Setup-Token", srv.token)
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func TestNewServerRejectsNonLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", "192.168.1.2:0", "[::1]:0", "nonsense"} {
		if _, _, err := newServer(addr, t.TempDir(), "/x"); err == nil {
			t.Errorf("newServer(%q) accepted", addr)
		}
	}
	if _, _, err := newServer("127.0.0.1:0", "relative/dir", "/x"); err == nil {
		t.Error("relative -dir accepted")
	}
}

func TestTokenLinkSetsCookieAndRedirects(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "http://"+srv.host+"/?token="+srv.token, nil)
	req.Host = srv.host
	rec := do(t, srv, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("status = %d, location = %q", rec.Code, rec.Header().Get("Location"))
	}
	cookie := rec.Result().Cookies()
	if len(cookie) != 1 || cookie[0].Value != srv.token || !cookie[0].HttpOnly || cookie[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie = %+v", cookie)
	}

	bad := httptest.NewRequest(http.MethodGet, "http://"+srv.host+"/?token=wrong", nil)
	bad.Host = srv.host
	if rec := do(t, srv, bad); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong token status = %d", rec.Code)
	}
}

func TestGuardRejectsMissingCookieWrongHostAndCrossOrigin(t *testing.T) {
	srv := testServer(t)

	noCookie := httptest.NewRequest(http.MethodGet, "http://"+srv.host+"/api/accounts", nil)
	noCookie.Host = srv.host
	if rec := do(t, srv, noCookie); rec.Code != http.StatusForbidden {
		t.Fatalf("no cookie = %d", rec.Code)
	}

	rebinding := authed(srv, http.MethodGet, "/api/accounts", nil)
	rebinding.Host = "evil.example:80"
	if rec := do(t, srv, rebinding); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign host = %d", rec.Code)
	}

	cross := authed(srv, http.MethodPost, "/api/accounts", strings.NewReader("{}"))
	cross.Header.Set("Origin", "http://evil.example")
	if rec := do(t, srv, cross); rec.Code != http.StatusForbidden {
		t.Fatalf("cross origin = %d", rec.Code)
	}

	noHeader := authed(srv, http.MethodPost, "/api/accounts", strings.NewReader("{}"))
	noHeader.Header.Del("X-Setup-Token")
	if rec := do(t, srv, noHeader); rec.Code != http.StatusForbidden {
		t.Fatalf("missing request token = %d", rec.Code)
	}
}

func TestIndexInjectsTokenAndSecurityHeaders(t *testing.T) {
	srv := testServer(t)
	rec := do(t, srv, authed(srv, http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `const TOKEN = "`+srv.token+`"`) {
		t.Fatal("token not injected")
	}
	for _, header := range []string{"Content-Security-Policy", "X-Frame-Options", "Cache-Control"} {
		if rec.Header().Get(header) == "" {
			t.Errorf("missing %s", header)
		}
	}
}

func TestSaveListDeleteNeverReturnsPassword(t *testing.T) {
	srv := testServer(t)
	body, _ := json.Marshal(map[string]any{
		"id": "label", "address": "hello@example.org", "imapServer": "mail.example.org",
		"permissions": []string{"mail.read"}, "readOnly": true, "password": "very-secret-pw",
	})
	rec := do(t, srv, authed(srv, http.MethodPost, "/api/accounts", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("save = %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "very-secret-pw") {
		t.Fatal("save response contains the password")
	}

	rec = do(t, srv, authed(srv, http.MethodGet, "/api/accounts", nil))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "very-secret-pw") {
		t.Fatalf("list = %d (password leaked: %t)", rec.Code, strings.Contains(rec.Body.String(), "very-secret-pw"))
	}
	var list struct {
		Accounts []struct {
			ServerName  string `json:"serverName"`
			HasPassword bool   `json:"hasPassword"`
			Command     string `json:"command"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Accounts) != 1 || list.Accounts[0].ServerName != "mail-label" || !list.Accounts[0].HasPassword ||
		!strings.Contains(list.Accounts[0].Command, `"command":"/opt/icloud-mcp"`) {
		t.Fatalf("list = %+v", list)
	}

	invalid := strings.NewReader(`{"id":"x","address":"a@b.c","permissions":["mail.read"],"password":"12345678","extra":1}`)
	if rec := do(t, srv, authed(srv, http.MethodPost, "/api/accounts", invalid)); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field = %d", rec.Code)
	}
	plain := authed(srv, http.MethodPost, "/api/accounts", strings.NewReader("{}"))
	plain.Header.Set("Content-Type", "text/plain")
	if rec := do(t, srv, plain); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain = %d", rec.Code)
	}

	if rec := do(t, srv, authed(srv, http.MethodDelete, "/api/accounts/label", nil)); rec.Code != http.StatusOK {
		t.Fatalf("delete = %d", rec.Code)
	}
	if rec := do(t, srv, authed(srv, http.MethodDelete, "/api/accounts/label", nil)); rec.Code != http.StatusNotFound {
		t.Fatalf("second delete = %d", rec.Code)
	}
}
