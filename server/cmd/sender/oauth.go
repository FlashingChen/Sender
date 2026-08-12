package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	oauthClientID = "sender-cli"
	// loginTimeout matches the server's authorizationCodeTTL (5 minutes) with
	// headroom for the browser round trip.
	loginTimeout = 6 * time.Minute
)

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

func randomBytes(n int) ([]byte, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("generate random bytes: %w", err)
	}
	return raw, nil
}

func base64URL(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

// pkcePair returns an S256 PKCE verifier and its challenge.
func pkcePair() (verifier, challenge string, err error) {
	raw, err := randomBytes(32)
	if err != nil {
		return "", "", err
	}
	verifier = base64URL(raw)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64URL(sum[:]), nil
}

func authorizeURL(server, redirectURI, challenge, state string) string {
	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", oauthClientID)
	values.Set("redirect_uri", redirectURI)
	values.Set("code_challenge", challenge)
	values.Set("code_challenge_method", "S256")
	values.Set("state", state)
	return strings.TrimRight(server, "/") + "/authorize?" + values.Encode()
}

type callbackResult struct {
	code       string
	oauthError string
}

// loopbackLogin runs the RFC 8252 loopback OAuth flow: it binds a local
// listener on 127.0.0.1, opens the server's authorize page in the browser,
// and waits for the redirect back after the user clicks 批准. No code ever
// needs to be copied out of the browser.
func loopbackLogin(server string, noBrowser bool, openBrowser func(string) error, stdout io.Writer) (Config, error) {
	verifier, challenge, err := pkcePair()
	if err != nil {
		return Config{}, err
	}
	stateBytes, err := randomBytes(16)
	if err != nil {
		return Config{}, err
	}
	state := hex.EncodeToString(stateBytes)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Config{}, fmt.Errorf("open loopback listener: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)
	authURL := authorizeURL(server, redirectURI, challenge, state)

	results := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		query := r.URL.Query()
		gotState := query.Get("state")
		oauthError := query.Get("error")
		code := query.Get("code")
		if !constantTimeEqual(gotState, state) {
			serveCallbackPage(w, http.StatusBadRequest, "授权失败", "state 不匹配，请重新运行 sender login。")
			results <- callbackResult{oauthError: "state mismatch"}
			return
		}
		if oauthError != "" {
			serveCallbackPage(w, http.StatusOK, "授权未完成", "你在授权页面选择了拒绝，可以关闭此页面。")
			results <- callbackResult{oauthError: oauthError}
			return
		}
		if code == "" {
			serveCallbackPage(w, http.StatusBadRequest, "授权失败", "回调缺少 code 参数，请重新运行 sender login。")
			results <- callbackResult{oauthError: "invalid_request"}
			return
		}
		serveCallbackPage(w, http.StatusOK, "授权成功", "已获得授权，可以关闭此页面并回到终端。")
		results <- callbackResult{code: code}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		serveCallbackPage(w, http.StatusOK, "Sender 授权回调", "等待浏览器跳转…")
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	fmt.Fprintf(stdout, "浏览器将打开授权页面；若未自动打开，请访问：\n%s\n", authURL)
	if !noBrowser && openBrowser != nil {
		if err := openBrowser(authURL); err != nil {
			fmt.Fprintf(stdout, "无法自动打开浏览器，请手动访问上面的链接。\n")
		}
	}

	var result callbackResult
	select {
	case result = <-results:
	case <-time.After(loginTimeout):
		return Config{}, errors.New("等待授权超时（6 分钟），请重新运行 sender login")
	}
	if result.oauthError != "" {
		return Config{}, fmt.Errorf("授权未完成：%s", result.oauthError)
	}

	token, err := exchangeCode(server, result.code, verifier, redirectURI)
	if err != nil {
		return Config{}, err
	}
	return Config{
		Server:      strings.TrimRight(server, "/"),
		AccessToken: token.AccessToken,
		ExpiresAt:   time.Now().Add(time.Duration(token.ExpiresIn) * time.Second),
	}, nil
}

func serveCallbackPage(w http.ResponseWriter, status int, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, "<!doctype html><html lang=\"zh-CN\"><head><meta charset=\"utf-8\"><title>%s</title></head><body><h1>%s</h1><p>%s</p></body></html>",
		title, title, body)
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func exchangeCode(server, code, verifier, redirectURI string) (tokenResponse, error) {
	body, err := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"code_verifier": verifier,
		"client_id":     oauthClientID,
		"redirect_uri":  redirectURI,
	})
	if err != nil {
		return tokenResponse{}, err
	}
	resp, err := http.Post(
		strings.TrimRight(server, "/")+"/api/v1/oauth/token",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("请求 token 端点: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("读取 token 响应: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return tokenResponse{}, fmt.Errorf("换取 token 失败（HTTP %d）：%s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	var token tokenResponse
	if err := json.Unmarshal(payload, &token); err != nil {
		return tokenResponse{}, fmt.Errorf("解析 token 响应: %w", err)
	}
	if token.AccessToken == "" {
		return tokenResponse{}, errors.New("token 响应缺少 access_token")
	}
	return token, nil
}
