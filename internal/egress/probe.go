package egress

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// ProbeEndpoint 是出口体检用的回显服务：返回 {"ip":"1.2.3.4"}。
// 走 https，顺带验证目标站点的 TLS 链路。
const ProbeEndpoint = "https://api.ipify.org?format=json"

// ProbeResult 是一次出口体检的结果。
type ProbeResult struct {
	OK        bool   `json:"ok"`
	IP        string `json:"ip,omitempty"`
	ElapsedMS int64  `json:"elapsedMs"`
	Error     string `json:"error,omitempty"`
}

// Probe 通过指定出口访问回显服务，拿到这条链路的真实出口 IP。
// proxyURL 为空表示直连（用于和代理出口做对比）。
func Probe(ctx context.Context, proxyURL string) ProbeResult {
	return ProbeEndpointURL(ctx, proxyURL, ProbeEndpoint)
}

// ProbeEndpointURL 允许自定义回显地址（测试用）。
func ProbeEndpointURL(ctx context.Context, proxyURL, endpoint string) ProbeResult {
	start := time.Now()
	base := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}
	proxy, err := Parse(proxyURL)
	if err != nil {
		return ProbeResult{ElapsedMS: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	transport := base
	if proxy != nil {
		transport, err = proxy.Transport(base)
		if err != nil {
			return ProbeResult{ElapsedMS: time.Since(start).Milliseconds(), Error: err.Error()}
		}
	}
	// 一次性探针不保留空闲连接，避免反复点「检测」堆积连接与 goroutine。
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ProbeResult{ElapsedMS: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return ProbeResult{ElapsedMS: time.Since(start).Milliseconds(), Error: trimErr(err)}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		return ProbeResult{ElapsedMS: elapsed, Error: trimErr(err)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ProbeResult{ElapsedMS: elapsed, Error: fmt.Sprintf("回显服务返回 HTTP %d", resp.StatusCode)}
	}
	ip := parseEchoIP(raw)
	if ip == "" {
		return ProbeResult{ElapsedMS: elapsed, Error: "回显内容里没有 IP: " + string(trimBytes(raw, 120))}
	}
	return ProbeResult{OK: true, IP: ip, ElapsedMS: elapsed}
}

func parseEchoIP(raw []byte) string {
	var body struct {
		IP string `json:"ip"`
	}
	if err := json.Unmarshal(raw, &body); err == nil && body.IP != "" {
		return strings.TrimSpace(body.IP)
	}
	text := strings.TrimSpace(string(raw))
	if net.ParseIP(text) != nil {
		return text
	}
	return ""
}

func trimErr(err error) string {
	return trimString(err.Error(), 240)
}

func trimString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func trimBytes(raw []byte, n int) []byte {
	if len(raw) <= n {
		return raw
	}
	return raw[:n]
}
