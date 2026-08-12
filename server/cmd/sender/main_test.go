package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testEnv(t *testing.T) (*cliEnv, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := newCLIEnv(&stdout, &stderr, func(string) string { return "" })
	env.configPath = filepath.Join(t.TempDir(), "config.json")
	return env, &stdout, &stderr
}

func writeTestConfig(t *testing.T, env *cliEnv, server, token string) {
	t.Helper()
	if err := saveConfig(env.configPath, Config{
		Server:      server,
		AccessToken: token,
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPKCEPair(t *testing.T) {
	verifier, challenge, err := pkcePair()
	if err != nil {
		t.Fatal(err)
	}
	if len(verifier) != 43 {
		t.Fatalf("verifier length = %d, want 43 (base64url of 32 bytes)", len(verifier))
	}
	sum := sha256.Sum256([]byte(verifier))
	if want := base64.RawURLEncoding.EncodeToString(sum[:]); challenge != want {
		t.Fatalf("challenge %q does not match S256(verifier) %q", challenge, want)
	}
	verifier2, _, err := pkcePair()
	if err != nil {
		t.Fatal(err)
	}
	if verifier2 == verifier {
		t.Fatal("two verifiers must differ")
	}
}

func TestAuthorizeURL(t *testing.T) {
	u, err := url.Parse(authorizeURL("http://localhost:8080/", "http://127.0.0.1:43123/callback", "challenge", "state"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/authorize" {
		t.Fatalf("path = %q, want /authorize", u.Path)
	}
	q := u.Query()
	if q.Get("response_type") != "code" || q.Get("client_id") != oauthClientID ||
		q.Get("code_challenge_method") != "S256" ||
		q.Get("code_challenge") != "challenge" || q.Get("state") != "state" ||
		q.Get("redirect_uri") != "http://127.0.0.1:43123/callback" {
		t.Fatalf("unexpected query: %v", q)
	}
}

type exchangeRequest struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
	ClientID     string `json:"client_id"`
	RedirectURI  string `json:"redirect_uri"`
}

// fakeBrowser returns an openBrowser func that simulates the user's browser:
// it parses the authorize URL and hits the loopback callback with the given
// query override, returning the code/state/error the server would have sent.
func fakeBrowser(t *testing.T, params url.Values) func(string) error {
	t.Helper()
	return func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		q := u.Query()
		if q.Get("response_type") != "code" || q.Get("client_id") != oauthClientID ||
			q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" ||
			q.Get("state") == "" || q.Get("redirect_uri") == "" {
			return fmt.Errorf("authorize URL missing parameters: %s", authURL)
		}
		redirect, err := url.Parse(q.Get("redirect_uri"))
		if err != nil {
			return err
		}
		if redirect.Hostname() != "127.0.0.1" || redirect.Port() == "" {
			return fmt.Errorf("redirect_uri must be a 127.0.0.1 loopback with port: %s", redirect)
		}
		callback := redirect
		callbackQuery := callback.Query()
		if params.Get("state") == "" {
			callbackQuery.Set("state", q.Get("state"))
		}
		for key, values := range params {
			for _, value := range values {
				callbackQuery.Set(key, value)
			}
		}
		if callbackQuery.Get("code") == "" && callbackQuery.Get("error") == "" {
			callbackQuery.Set("code", "test-code-123")
		}
		callback.RawQuery = callbackQuery.Encode()
		resp, err := http.Get(callback.String())
		if err != nil {
			return err
		}
		resp.Body.Close()
		return nil
	}
}

func TestLoginLoopbackEndToEnd(t *testing.T) {
	var got exchangeRequest
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/oauth/token" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode exchange request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"tok-abc","token_type":"Bearer","expires_in":604800}`)
	}))
	defer tokenServer.Close()

	env, stdout, stderr := testEnv(t)
	var challengeFromURL string
	env.openBrowser = func(authURL string) error {
		u, _ := url.Parse(authURL)
		challengeFromURL = u.Query().Get("code_challenge")
		return fakeBrowser(t, nil)(authURL)
	}
	if code := cmdLogin(env, []string{"--server", tokenServer.URL}); code != 0 {
		t.Fatalf("login exit %d: %s", code, stderr.String())
	}

	if got.GrantType != "authorization_code" || got.Code != "test-code-123" {
		t.Fatalf("exchange body wrong: %+v", got)
	}
	if got.ClientID != oauthClientID || got.RedirectURI == "" {
		t.Fatalf("exchange body wrong: %+v", got)
	}
	sum := sha256.Sum256([]byte(got.CodeVerifier))
	if want := base64.RawURLEncoding.EncodeToString(sum[:]); want != challengeFromURL {
		t.Fatalf("code_verifier %q does not match the challenge %q sent to /authorize", got.CodeVerifier, challengeFromURL)
	}

	cfg, err := loadConfig(env.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != tokenServer.URL || cfg.AccessToken != "tok-abc" {
		t.Fatalf("config = %+v", cfg)
	}
	if !cfg.ExpiresAt.After(time.Now().Add(6*24*time.Hour)) || cfg.ExpiresAt.After(time.Now().Add(8*24*time.Hour)) {
		t.Fatalf("expires_at = %v, want ~7 days from now", cfg.ExpiresAt)
	}
	if !strings.Contains(stdout.String(), "登录成功") {
		t.Fatalf("stdout missing success message: %s", stdout.String())
	}
}

func TestLoginStateMismatch(t *testing.T) {
	env, _, stderr := testEnv(t)
	env.openBrowser = fakeBrowser(t, url.Values{"state": {"wrong-state"}})
	if code := cmdLogin(env, []string{"--server", "http://127.0.0.1:1"}); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "state") {
		t.Fatalf("stderr = %s, want state mismatch error", stderr.String())
	}
	if _, err := os.Stat(env.configPath); !os.IsNotExist(err) {
		t.Fatal("config must not be written on failed login")
	}
}

func TestLoginDenied(t *testing.T) {
	env, _, stderr := testEnv(t)
	env.openBrowser = fakeBrowser(t, url.Values{"error": {"access_denied"}})
	if code := cmdLogin(env, []string{"--server", "http://127.0.0.1:1"}); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "access_denied") {
		t.Fatalf("stderr = %s, want access_denied", stderr.String())
	}
	if _, err := os.Stat(env.configPath); !os.IsNotExist(err) {
		t.Fatal("config must not be written on denied login")
	}
}

func TestLoginTokenEndpointError(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_grant"}`)
	}))
	defer tokenServer.Close()
	env, _, stderr := testEnv(t)
	env.openBrowser = fakeBrowser(t, nil)
	if code := cmdLogin(env, []string{"--server", tokenServer.URL}); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "invalid_grant") {
		t.Fatalf("stderr = %s, want invalid_grant", stderr.String())
	}
}

func TestMessagesCommandJSON(t *testing.T) {
	const payload = `{"messages":[{"id":1,"client_msg_id":"c:1","app":"com.tencent.mm","app_name":"微信","chat":"张三","sender":"张三","content":"今晚吃饭吗","ts":1785690100,"day":"2026-08-12"}],"next_cursor":"1785690100:1"}`
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/messages" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-123" {
			t.Errorf("Authorization = %q", got)
		}
		q := r.URL.Query()
		if q.Get("day") != "2026-08-12" || q.Get("app") != "com.tencent.mm" ||
			q.Get("device_id") != "d1" || q.Get("limit") != "42" ||
			q.Get("cursor") != "1785690100:1" {
			t.Errorf("query = %v", q)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, payload)
	}))
	defer api.Close()

	env, stdout, stderr := testEnv(t)
	writeTestConfig(t, env, api.URL, "tok-123")
	code := cmdMessages(env, []string{
		"--day", "2026-08-12", "--app", "com.tencent.mm", "--device-id", "d1",
		"--limit", "42", "--cursor", "1785690100:1",
	})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != payload {
		t.Fatalf("stdout = %s, want raw API payload", stdout.String())
	}
}

func TestMessagesCommandText(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"messages":[{"app":"com.tencent.mm","app_name":"微信","sender":"张三","content":"今晚吃饭吗","ts":1785690100}],"next_cursor":"1785690100:1"}`)
	}))
	defer api.Close()
	env, stdout, stderr := testEnv(t)
	writeTestConfig(t, env, api.URL, "tok")
	if code := cmdMessages(env, []string{"--text"}); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"微信", "张三", "今晚吃饭吗", "1785690100:1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %s, want %q", out, want)
		}
	}
}

func TestMessagesTokenOverrideSkipsConfig(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer direct-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"messages":[],"next_cursor":""}`)
	}))
	defer api.Close()
	env, _, stderr := testEnv(t) // no config file
	if code := cmdMessages(env, []string{"--server", api.URL, "--token", "direct-token"}); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
}

func TestMessagesNotLoggedIn(t *testing.T) {
	env, _, stderr := testEnv(t) // no config, no token
	if code := cmdMessages(env, nil); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "sender login") {
		t.Fatalf("stderr = %s, want login hint", stderr.String())
	}
}

func TestAppsCommand(t *testing.T) {
	const payload = `[{"app":"com.tencent.mm","app_name":"微信","count":12,"last_ts":1785690100}]`
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/apps" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("day") != "2026-08-12" || q.Get("device_id") != "d1" {
			t.Errorf("query = %v", q)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, payload)
	}))
	defer api.Close()
	env, stdout, stderr := testEnv(t)
	writeTestConfig(t, env, api.URL, "tok")
	if code := cmdApps(env, []string{"--day", "2026-08-12", "--device-id", "d1"}); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != payload {
		t.Fatalf("stdout = %s, want raw payload", stdout.String())
	}
	if code := cmdApps(env, []string{"--text", "--day", "2026-08-12", "--device-id", "d1"}); code != 0 {
		t.Fatalf("text exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "微信") || !strings.Contains(stdout.String(), "com.tencent.mm") {
		t.Fatalf("text stdout = %s", stdout.String())
	}
}

func TestConfigRoundTripAndPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		return // POSIX permission bits are not enforced the same way
	}
	env, _, _ := testEnv(t)
	cfg := Config{Server: "http://localhost:8080", AccessToken: "tok", ExpiresAt: time.Now()}
	if err := saveConfig(env.configPath, cfg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(env.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("config permissions = %o, want 0600", perm)
	}
	loaded, err := loadConfig(env.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server != cfg.Server || loaded.AccessToken != cfg.AccessToken || !loaded.ExpiresAt.Equal(cfg.ExpiresAt) {
		t.Fatalf("loaded = %+v, want %+v", loaded, cfg)
	}
}

func TestLoadConfigMissing(t *testing.T) {
	env, _, _ := testEnv(t)
	if _, err := loadConfig(env.configPath); err != errNotLoggedIn {
		t.Fatalf("err = %v, want errNotLoggedIn", err)
	}
}

func TestLogout(t *testing.T) {
	env, stdout, stderr := testEnv(t)
	writeTestConfig(t, env, "http://localhost:8080", "tok")
	if code := cmdLogout(env, nil); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "已登出") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if _, err := os.Stat(env.configPath); !os.IsNotExist(err) {
		t.Fatal("config file must be removed")
	}
	if code := cmdLogout(env, nil); code != 1 {
		t.Fatalf("second logout exit = %d, want 1", code)
	}
}

func TestStatus(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer api.Close()
	env, stdout, stderr := testEnv(t)
	writeTestConfig(t, env, api.URL, "tok")
	if code := cmdStatus(env, nil); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	for _, want := range []string{"已登录", "健康检查：  ok", "Token 过期"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %s, want %q", stdout.String(), want)
		}
	}
}

func TestStatusServerDown(t *testing.T) {
	env, _, stderr := testEnv(t)
	writeTestConfig(t, env, "http://127.0.0.1:1", "tok")
	if code := cmdStatus(env, nil); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "健康检查") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestStatusNotLoggedIn(t *testing.T) {
	env, _, stderr := testEnv(t)
	if code := cmdStatus(env, nil); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "sender login") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestRunDispatch(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help exit %d", code)
	}
	if !strings.Contains(stdout.String(), "sender login") {
		t.Fatalf("help stdout = %s", stdout.String())
	}
	stderr.Reset()
	if code := run([]string{"nonsense"}, &stdout, &stderr); code != 2 {
		t.Fatalf("unknown command exit = %d, want 2", code)
	}
}
