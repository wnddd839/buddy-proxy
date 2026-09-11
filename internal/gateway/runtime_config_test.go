package gateway

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

func TestRaceRuntimeConfigAPIKeyAndSite(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	t.Setenv("CODEBUDDY_PROXY_ENV_FILE", envFile)

	cfg := config.Config{
		Host:          "127.0.0.1",
		Port:          32126,
		APIKey:        "old-key",
		RequireAPIKey: true,
		Site:          "domestic",
		Product:       "codebuddy",
		BaseURL:       "https://www.codebuddy.cn",
	}
	svc := New(cfg, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			i := 0
			for {
				select {
				case <-stop:
					return
				default:
					i++
					svc.SetAPIKey(fmt.Sprintf("key-%d", i))
					_, _ = svc.SetPoolSite("global")
					_, _ = svc.SetPoolSite("domestic")
					_, _ = svc.SetPoolProduct("workbuddy")
					_, _ = svc.SetPoolProduct("codebuddy")
				}
			}
		})
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					c := svc.Config()
					_ = c.APIKey
					_ = c.Site
					_ = svc.ActivePoolSite()
					_ = svc.Status()
				}
			}
		})
	}
	time.Sleep(250 * time.Millisecond)
	close(stop)
	wg.Wait()
}
