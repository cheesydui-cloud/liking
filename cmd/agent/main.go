package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"liking/internal/agent"
	"liking/internal/version"
)

func main() {
	var (
		connect, token, tokenFile, dir string
		insecure                       bool
	)
	fs := flag.NewFlagSet("liking-agent", flag.ExitOnError)
	fs.StringVar(&connect, "connect", "", "panel WebSocket URL (ws:// or wss://…/v1/agents)")
	fs.StringVar(&token, "token", "", "server token")
	fs.StringVar(&tokenFile, "token-file", "/etc/liking/panel.token", "token file if --token is empty")
	fs.StringVar(&dir, "dir", "/var/lib/liking", "config/data directory")
	fs.BoolVar(&insecure, "insecure-connect", false, "allow plaintext ws://")
	showVer := fs.Bool("version", false, "print version")
	_ = fs.Parse(os.Args[1:])
	if *showVer {
		fmt.Println(version.Version)
		return
	}
	if connect == "" {
		fmt.Fprintln(os.Stderr, "需要 --connect wss://面板/v1/agents")
		os.Exit(2)
	}
	if token == "" {
		b, err := os.ReadFile(tokenFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "读取 token:", err)
			os.Exit(1)
		}
		token = strings.TrimSpace(string(b))
	}
	if token == "" {
		fmt.Fprintln(os.Stderr, "token 为空")
		os.Exit(1)
	}
	if err := validateConnect(connect, insecure); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := agent.Run(ctx, agent.Config{
		ConnectURL: connect,
		Token:      token,
		Dir:        dir,
		Insecure:   insecure,
	}); err != nil && err != context.Canceled {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func validateConnect(raw string, allowInsecure bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("--connect 无法解析: %v", err)
	}
	switch u.Scheme {
	case "wss", "https":
		return nil
	case "ws", "http":
		if allowInsecure {
			fmt.Fprintln(os.Stderr, "警告: 明文 ws:// 控制信道，仅限本地测试")
			return nil
		}
		return fmt.Errorf("--connect 必须使用 wss://；本地测试请加 --insecure-connect")
	default:
		return fmt.Errorf("--connect 协议必须是 wss://（当前 %q）", u.Scheme)
	}
}
