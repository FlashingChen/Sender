// Command sender is the agent-facing CLI for the Sender server. It wraps the
// OAuth authorization-code + PKCE flow and the query API so agents do not
// need to speak HTTP directly.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
)

func defaultOpenBrowser(url string) error {
	var command []string
	switch runtime.GOOS {
	case "darwin":
		command = []string{"open", url}
	case "linux":
		command = []string{"xdg-open", url}
	case "windows":
		command = []string{"rundll32", "url.dll,FileProtocolHandler", url}
	default:
		return fmt.Errorf("unsupported platform: open %s manually", url)
	}
	return exec.Command(command[0], command[1:]...).Start()
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `Sender CLI —— agent 直接查消息，不用手写 HTTP

用法：
  sender login    [--server URL] [--config PATH] [--no-browser]
                        OAuth 登录：浏览器打开授权页，点「批准」即完成，
                        自动回跳到本机回调，无需复制粘贴 code
  sender logout   [--config PATH]
                        删除本地登录凭证
  sender status   [--config PATH]
                        查看登录状态、token 过期时间与服务端健康
  sender messages [--day YYYY-MM-DD] [--app PKG] [--device-id ID]
                  [--limit N] [--cursor C] [--text]
                        查询消息（默认输出 JSON，--text 输出表格）
  sender apps     [--day YYYY-MM-DD] [--device-id ID] [--text]
                        按 App 聚合统计（默认输出 JSON）
  sender help
                        显示本帮助

环境变量：
  SENDER_SERVER   服务器地址（默认 http://localhost:8080）
  SENDER_TOKEN    直接提供 access token，跳过配置文件（agent 无状态调用）
  SENDER_CONFIG   配置文件路径（默认 ~/.config/sender/config.json）
每个子命令用 --help 查看详细参数。`)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	env := newCLIEnv(stdout, stderr, nil)
	switch args[0] {
	case "login":
		return cmdLogin(env, args[1:])
	case "logout":
		return cmdLogout(env, args[1:])
	case "status":
		return cmdStatus(env, args[1:])
	case "messages":
		return cmdMessages(env, args[1:])
	case "apps":
		return cmdApps(env, args[1:])
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "sender: unknown command %q\n", args[0])
		usage(stderr)
		return 2
	}
}
