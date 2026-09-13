package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/gateway"
	"github.com/wnddd839/codebuddy-proxy/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel()}))
	cfg := config.Load()
	generatedKey := false

	// First-run / binary UX: if no gateway key is configured, create one and
	// persist it to .env so clients don't break after restart.
	if strings.TrimSpace(cfg.APIKey) == "" {
		key, err := server.GenerateProxyAPIKey()
		if err != nil {
			logger.Error("failed to generate default api key", "error", err)
			os.Exit(1)
		}
		envPath := config.ResolveEnvFilePath()
		values := map[string]string{
			"CODEBUDDY_PROXY_API_KEY":         key,
			"CODEBUDDY_PROXY_REQUIRE_API_KEY": "true",
		}
		if err := config.UpsertEnvFile(envPath, values); err != nil {
			logger.Error("failed to persist default api key", "path", envPath, "error", err)
			os.Exit(1)
		}
		cfg.APIKey = key
		cfg.RequireAPIKey = true
		_ = os.Setenv("CODEBUDDY_PROXY_API_KEY", key)
		_ = os.Setenv("CODEBUDDY_PROXY_REQUIRE_API_KEY", "true")
		generatedKey = true
		logger.Debug("generated and saved gateway api key", "path", envPath)
	}

	svc := gateway.New(cfg, logger)
	srv := server.New(cfg, svc)

	printStartupBanner(cfg, generatedKey)
	logger.Debug("codebuddy proxy starting",
		"addr", "http://"+cfg.Addr()+"/v1",
		"admin", adminURL(cfg),
		"transport", cfg.Transport,
		"accountsPath", cfg.AccountsPath,
		"requireApiKey", cfg.RequireAPIKey,
	)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err != nil {
			logger.Error("server exited", "error", err)
			os.Exit(1)
		}
	case sig := <-sigCh:
		logger.Info("shutting down", "signal", sig.String())
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("shutdown error", "error", err)
			os.Exit(1)
		}
	}
}

func logLevel() slog.Level {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CODEBUDDY_PROXY_LOG_LEVEL"))) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "error":
		return slog.LevelError
	default:
		// 二进制默认安静：启动提示走 printStartupBanner，排障时设 CODEBUDDY_PROXY_LOG_LEVEL=info|debug
		return slog.LevelWarn
	}
}

func adminURL(cfg config.Config) string {
	if base := strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/"); base != "" {
		return base + "/direct-admin/"
	}
	return "http://" + cfg.Addr() + "/direct-admin/"
}

func printStartupBanner(cfg config.Config, generatedKey bool) {
	admin := adminURL(cfg)
	fmt.Fprintf(os.Stdout, `
CodeBuddy Proxy 已启动

  管理台（OAuth 登录、号池、API Key、客户端接入）:
  %s

`, admin)
	if generatedKey {
		fmt.Fprintf(os.Stdout, "  首次启动已生成网关 API Key 并写入 ~/.codebuddy/proxy.env，请在管理台「OpenAI 兼容接入」中查看/复制。\n\n")
	} else {
		fmt.Fprintf(os.Stdout, "  请在管理台完成 OAuth 后，在「OpenAI 兼容接入」复制 Base URL 与 API Key 给客户端。\n\n")
	}
}
