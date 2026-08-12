package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// cliEnv carries the per-invocation settings shared by every subcommand. It
// is built from flags, environment variables, and the config file so commands
// are testable without touching process state.
type cliEnv struct {
	server      string
	token       string
	configPath  string
	noBrowser   bool
	openBrowser func(string) error
	stdout      io.Writer
	stderr      io.Writer
	getenv      func(string) string
}

func newCLIEnv(stdout, stderr io.Writer, getenv func(string) string) *cliEnv {
	if getenv == nil {
		getenv = os.Getenv
	}
	env := &cliEnv{stdout: stdout, stderr: stderr, getenv: getenv}
	env.openBrowser = defaultOpenBrowser
	env.configPath = env.getenv("SENDER_CONFIG")
	if env.configPath == "" {
		env.configPath = defaultConfigPath()
	}
	return env
}

// flagServer registers the --server flag with SENDER_SERVER as its default.
func (e *cliEnv) flagServer(fs *flag.FlagSet) {
	fs.StringVar(&e.server, "server", e.getenv("SENDER_SERVER"), "服务器地址（默认 http://localhost:8080）")
}

func (e *cliEnv) flagConfig(fs *flag.FlagSet) {
	fs.StringVar(&e.configPath, "config", e.configPath, "配置文件路径（默认 ~/.config/sender/config.json）")
}

func (e *cliEnv) flagToken(fs *flag.FlagSet) {
	fs.StringVar(&e.token, "token", e.getenv("SENDER_TOKEN"), "直接提供 access token，跳过读取配置文件")
}

// resolveServer picks the effective server URL: explicit flag / SENDER_SERVER
// wins, then the config file's server (when logged in), then the default.
func (e *cliEnv) resolveServer(cfg Config) string {
	if e.server != "" {
		return strings.TrimRight(e.server, "/")
	}
	if cfg.Server != "" {
		return strings.TrimRight(cfg.Server, "/")
	}
	return "http://localhost:8080"
}

func (e *cliEnv) failf(format string, args ...any) int {
	fmt.Fprintf(e.stderr, "sender: "+format+"\n", args...)
	return 1
}

func cmdLogin(env *cliEnv, args []string) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(env.stderr)
	env.flagServer(fs)
	env.flagConfig(fs)
	fs.BoolVar(&env.noBrowser, "no-browser", false, "不自动打开浏览器，只打印授权链接")
	fs.Usage = func() {
		fmt.Fprintln(env.stderr, "用法：sender login [--server URL] [--config PATH] [--no-browser]")
		fmt.Fprintln(env.stderr, "浏览器打开授权页后点「批准」即完成登录，无需复制 code。")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		return env.failf("多余的参数：%s", strings.Join(fs.Args(), " "))
	}
	cfg, _ := loadConfig(env.configPath) // 已登录时复用其中的服务器地址
	server := env.resolveServer(cfg)
	fmt.Fprintf(env.stdout, "Sender CLI · 登录 %s\n", server)
	cfg, err := loopbackLogin(server, env.noBrowser, env.openBrowser, env.stdout)
	if err != nil {
		return env.failf("%v", err)
	}
	if err := saveConfig(env.configPath, cfg); err != nil {
		return env.failf("%v", err)
	}
	fmt.Fprintf(env.stdout, "登录成功：token 已保存到 %s（7 天有效）\n", env.configPath)
	return 0
}

func cmdLogout(env *cliEnv, args []string) int {
	fs := flag.NewFlagSet("logout", flag.ContinueOnError)
	fs.SetOutput(env.stderr)
	env.flagConfig(fs)
	fs.Usage = func() {
		fmt.Fprintln(env.stderr, "用法：sender logout [--config PATH]")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		return env.failf("多余的参数：%s", strings.Join(fs.Args(), " "))
	}
	if err := deleteConfig(env.configPath); err != nil {
		return env.failf("%v", err)
	}
	fmt.Fprintf(env.stdout, "已登出：%s\n", env.configPath)
	return 0
}

func cmdStatus(env *cliEnv, args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(env.stderr)
	env.flagServer(fs)
	env.flagConfig(fs)
	fs.Usage = func() {
		fmt.Fprintln(env.stderr, "用法：sender status [--config PATH]")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		return env.failf("多余的参数：%s", strings.Join(fs.Args(), " "))
	}
	cfg, err := loadConfig(env.configPath)
	if err != nil {
		return env.failf("%v", err)
	}
	server := env.resolveServer(cfg)
	fmt.Fprintf(env.stdout, "服务器：    %s\n", server)
	fmt.Fprintf(env.stdout, "已登录：    是\n")
	if time.Now().After(cfg.ExpiresAt) {
		fmt.Fprintf(env.stdout, "Token 过期：%s（已过期，请重新 sender login）\n", cfg.ExpiresAt.Format("2006-01-02 15:04:05"))
		return 1
	}
	fmt.Fprintf(env.stdout, "Token 过期：%s\n", cfg.ExpiresAt.Format("2006-01-02 15:04:05"))
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(server + "/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		return env.failf("健康检查失败：%v", err)
	}
	resp.Body.Close()
	fmt.Fprintln(env.stdout, "健康检查：  ok")
	return 0
}

// loggedInToken returns the effective access token: the --token flag /
// SENDER_TOKEN env wins, otherwise the config file. A missing credential is
// a hard error so agents fail loudly instead of getting 401s.
func (e *cliEnv) loggedInToken(cfg *Config, cfgErr error) (string, error) {
	if e.token != "" {
		return e.token, nil
	}
	if cfgErr != nil {
		return "", cfgErr
	}
	return cfg.AccessToken, nil
}

func apiGet(server, path, token string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(server, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d：%s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	return payload, nil
}

type messageRecord struct {
	AppName string `json:"app_name"`
	Sender  string `json:"sender"`
	Content string `json:"content"`
	TS      int64  `json:"ts"`
}

type appSummary struct {
	App     string `json:"app"`
	AppName string `json:"app_name"`
	Count   int64  `json:"count"`
	LastTS  int64  `json:"last_ts"`
}

func cmdMessages(env *cliEnv, args []string) int {
	fs := flag.NewFlagSet("messages", flag.ContinueOnError)
	fs.SetOutput(env.stderr)
	env.flagServer(fs)
	env.flagToken(fs)
	env.flagConfig(fs)
	var (
		day      string
		app      string
		deviceID string
		cursor   string
		limit    int
		text     bool
	)
	fs.StringVar(&day, "day", "", "按天过滤（YYYY-MM-DD；省略则返回最近消息）")
	fs.StringVar(&app, "app", "", "按 App 包名过滤")
	fs.StringVar(&deviceID, "device-id", "", "按设备 ID 过滤")
	fs.IntVar(&limit, "limit", 100, "条数上限（1-500）")
	fs.StringVar(&cursor, "cursor", "", "上一页返回的 next_cursor（需配合 --day）")
	fs.BoolVar(&text, "text", false, "人类可读表格输出（默认 JSON）")
	fs.Usage = func() {
		fmt.Fprintln(env.stderr, "用法：sender messages [--day YYYY-MM-DD] [--app PKG] [--device-id ID] [--limit N] [--cursor C] [--text]")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		return env.failf("多余的参数：%s", strings.Join(fs.Args(), " "))
	}
	cfg, cfgErr := loadConfig(env.configPath)
	token, err := env.loggedInToken(&cfg, cfgErr)
	if err != nil {
		return env.failf("%v", err)
	}
	query := url.Values{}
	if day != "" {
		query.Set("day", day)
	}
	if app != "" {
		query.Set("app", app)
	}
	if deviceID != "" {
		query.Set("device_id", deviceID)
	}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	payload, err := apiGet(env.resolveServer(cfg), "/api/v1/messages?"+query.Encode(), token)
	if err != nil {
		return env.failf("%v", err)
	}
	if text {
		if err := printMessagesText(env.stdout, payload); err != nil {
			return env.failf("%v", err)
		}
		return 0
	}
	fmt.Fprintln(env.stdout, string(payload))
	return 0
}

func printMessagesText(w io.Writer, payload []byte) error {
	var resp struct {
		Messages   []messageRecord `json:"messages"`
		NextCursor string          `json:"next_cursor"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return fmt.Errorf("解析消息响应: %w", err)
	}
	for _, m := range resp.Messages {
		fmt.Fprintf(w, "%s  %s  %s  %s\n",
			time.Unix(m.TS, 0).Format("2006-01-02 15:04:05"),
			m.AppName, m.Sender, truncateRunes(m.Content, 100))
	}
	if resp.NextCursor != "" {
		fmt.Fprintf(w, "# 下一页：sender messages --cursor %s\n", resp.NextCursor)
	}
	return nil
}

func cmdApps(env *cliEnv, args []string) int {
	fs := flag.NewFlagSet("apps", flag.ContinueOnError)
	fs.SetOutput(env.stderr)
	env.flagServer(fs)
	env.flagToken(fs)
	env.flagConfig(fs)
	var (
		day      string
		deviceID string
		text     bool
	)
	fs.StringVar(&day, "day", "", "按天过滤（YYYY-MM-DD；省略则统计全部）")
	fs.StringVar(&deviceID, "device-id", "", "按设备 ID 过滤")
	fs.BoolVar(&text, "text", false, "人类可读表格输出（默认 JSON）")
	fs.Usage = func() {
		fmt.Fprintln(env.stderr, "用法：sender apps [--day YYYY-MM-DD] [--device-id ID] [--text]")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		return env.failf("多余的参数：%s", strings.Join(fs.Args(), " "))
	}
	cfg, cfgErr := loadConfig(env.configPath)
	token, err := env.loggedInToken(&cfg, cfgErr)
	if err != nil {
		return env.failf("%v", err)
	}
	query := url.Values{}
	if day != "" {
		query.Set("day", day)
	}
	if deviceID != "" {
		query.Set("device_id", deviceID)
	}
	payload, err := apiGet(env.resolveServer(cfg), "/api/v1/apps?"+query.Encode(), token)
	if err != nil {
		return env.failf("%v", err)
	}
	if text {
		if err := printAppsText(env.stdout, payload); err != nil {
			return env.failf("%v", err)
		}
		return 0
	}
	fmt.Fprintln(env.stdout, string(payload))
	return 0
}

func printAppsText(w io.Writer, payload []byte) error {
	var apps []appSummary
	if err := json.Unmarshal(payload, &apps); err != nil {
		return fmt.Errorf("解析 apps 响应: %w", err)
	}
	for _, a := range apps {
		fmt.Fprintf(w, "%s  %s  %d  %s\n",
			a.AppName, a.App, a.Count, time.Unix(a.LastTS, 0).Format("2006-01-02 15:04:05"))
	}
	return nil
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
