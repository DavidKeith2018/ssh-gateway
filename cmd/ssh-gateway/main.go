package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	gateway "ssh-gateway"
	"ssh-gateway/updater"

	"golang.org/x/crypto/ssh"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, input io.Reader, output, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("ssh-gateway", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	replaceBackup := flags.Bool("replace", false, "恢复备份时允许替换现有数据（原数据自动保留）")
	dir := flags.String("data", "./data", "数据目录")
	listen := flags.String("listen", "127.0.0.1:2222", "SSH 监听地址")
	webListen := flags.String("web", "127.0.0.1:8080", "网页监听地址")
	publicOrigin := flags.String("web-origin", "", "反向代理后的固定网页来源，例如 https://ssh.example.com")
	certFile := flags.String("tls-cert", "", "网页 HTTPS 证书文件")
	keyFile := flags.String("tls-key", "", "网页 HTTPS 私钥文件")
	flags.Usage = func() {
		fmt.Fprintln(diagnostics, "用法：ssh-gateway [-data 目录] [-listen 地址] <命令>")
		fmt.Fprintln(diagnostics, "命令：version | serve | put（标准输入 JSON）| list | delete ID | test ID | probe 主机:端口 | admin-password（标准输入密码）| backup-check 文件 | backup-restore 文件（标准输入备份密码）")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	command := flags.Args()
	if len(command) == 0 {
		flags.Usage()
		return fmt.Errorf("请指定命令")
	}
	wantArgs := map[string]int{"version": 1, "serve": 1, "put": 1, "list": 1, "delete": 2, "test": 2, "probe": 2, "admin-password": 1, "backup-check": 2, "backup-restore": 2}
	if n, ok := wantArgs[command[0]]; !ok || len(command) != n {
		return fmt.Errorf("未知命令或参数数量不正确；使用 -h 查看用法")
	}
	if command[0] == "version" {
		fmt.Fprintln(output, updater.Version)
		return nil
	}
	if command[0] == "probe" {
		fp, err := gateway.ProbeFingerprint(ctx, command[1])
		if err != nil {
			return err
		}
		fmt.Fprintln(output, fp)
		fmt.Fprintln(diagnostics, "请通过可信渠道核对目标主机指纹后再录入。")
		return nil
	}
	if command[0] == "serve" {
		lock, err := gateway.AcquireInstance(*dir)
		if err != nil {
			return err
		}
		defer lock.Close()
	}
	if command[0] == "backup-check" || command[0] == "backup-restore" {
		password, err := io.ReadAll(io.LimitReader(input, 1026))
		if err != nil {
			return err
		}
		defer clear(password)
		file, err := os.Open(command[1])
		if err != nil {
			return err
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, (64<<20)+53))
		if err != nil {
			return err
		}
		secret := strings.TrimRight(string(password), "\r\n")
		if command[0] == "backup-check" {
			info, err := gateway.InspectBackup(ctx, data, secret)
			if err != nil {
				return err
			}
			return json.NewEncoder(output).Encode(info)
		}
		if _, err := os.Stat(filepath.Join(*dir, "gateway.db")); err == nil && !*replaceBackup {
			return fmt.Errorf("数据目录已存在数据库；确认覆盖请添加 -replace，原数据会自动保留")
		}
		previous, err := gateway.RestoreBackup(ctx, *dir, data, secret)
		if err != nil {
			return err
		}
		fmt.Fprintln(output, "恢复完成，原数据保留在：", previous)
		fmt.Fprintln(output, "请重新启动服务；启用主密码的备份仍需原主密码解锁。")
		return nil
	}
	store, err := gateway.OpenStore(*dir)
	if err != nil {
		return err
	}
	defer store.Close()
	if store.MasterPasswordStatus().Locked && command[0] != "serve" && command[0] != "admin-password" {
		return fmt.Errorf("凭证已锁定，请启动 serve 并在管理页面解锁后操作")
	}
	if store.MasterPasswordStatus().Locked && command[0] == "serve" {
		fmt.Fprintln(diagnostics, "主密码保护已启用：请在管理页面输入主密码解锁，SSH 中转和自动端口映射在解锁后可用")
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	switch command[0] {
	case "admin-password":
		data, err := io.ReadAll(io.LimitReader(input, 1024))
		if err != nil {
			return err
		}
		if err := store.SetAdminPassword(ctx, strings.TrimRight(string(data), "\r\n")); err != nil {
			return err
		}
		fmt.Fprintln(output, "管理员密码已更新，已有网页登录将失效")
		return nil
	case "put":
		decoder := json.NewDecoder(io.LimitReader(input, 1<<20))
		decoder.DisallowUnknownFields()
		var in gateway.PutInput
		if err := decoder.Decode(&in); err != nil {
			return fmt.Errorf("配置 JSON 无效：%w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return fmt.Errorf("只允许一个 JSON 对象")
		}
		result, err := store.PutTarget(ctx, in)
		if err != nil {
			return err
		}
		return encoder.Encode(result)
	case "list":
		targets, err := store.List(ctx)
		if err != nil {
			return err
		}
		return encoder.Encode(targets)
	case "delete":
		return store.Delete(ctx, command[1])
	case "test":
		if err := store.CheckTarget(ctx, command[1]); err != nil {
			return err
		}
		fmt.Fprintln(output, "目标 SSH 连接成功")
		return nil
	case "serve":
		if (*certFile == "") != (*keyFile == "") {
			return fmt.Errorf("HTTPS 证书与私钥必须同时提供")
		}
		signer, err := gateway.LoadHostKey(*dir)
		if err != nil {
			return err
		}
		listener, err := net.Listen("tcp", *listen)
		if err != nil {
			return err
		}
		defer listener.Close()
		webListener, err := net.Listen("tcp", *webListen)
		if err != nil {
			return err
		}
		defer webListener.Close()
		scheme := "http"
		if *certFile != "" {
			certificate, err := tls.LoadX509KeyPair(*certFile, *keyFile)
			if err != nil {
				return err
			}
			webListener = tls.NewListener(webListener, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
			scheme = "https"
		}
		password, err := store.EnsureAdmin(ctx)
		if err != nil {
			return err
		}
		if password != "" {
			fmt.Fprintln(diagnostics, "首次启动的管理员密码（仅展示一次）：", password)
		}
		fmt.Fprintf(diagnostics, "SSH 中转监听：%s\n主机指纹：%s\n", listener.Addr(), ssh.FingerprintSHA256(signer.PublicKey()))
		fmt.Fprintf(diagnostics, "管理页面：%s://%s\n", scheme, webListener.Addr())
		return serve(ctx, store, signer, listener, webListener, *publicOrigin)
	}
	return nil
}

func serve(parent context.Context, store *gateway.Store, signer ssh.Signer, sshListener, webListener net.Listener, publicOrigin string) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	web := gateway.NewWeb(ctx, store, signer, sshListener.Addr().String())
	if err := web.SetPublicOrigin(publicOrigin); err != nil {
		return err
	}
	server := &http.Server{
		Handler: web.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	results := make(chan error, 2)
	go func() { results <- gateway.NewServer(store, signer).Serve(ctx, sshListener) }()
	go func() { results <- server.Serve(webListener) }()
	var result error
	completed := 0
	select {
	case <-ctx.Done():
	case result = <-results:
		completed++
	}
	cancel()
	sshListener.Close()
	shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		server.Close()
	}
	for completed < 2 {
		err := <-results
		completed++
		if result == nil && err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			result = err
		}
	}
	if errors.Is(result, http.ErrServerClosed) || errors.Is(result, net.ErrClosed) {
		return nil
	}
	return result
}
