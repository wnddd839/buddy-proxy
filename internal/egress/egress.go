// Package egress 把「出口」抽象成可复用的 dialer / http.Client。
//
// 支持的 URL 形态：
//
//	http://host:port          普通 HTTP 代理
//	http://user:pass@host:port  带 basic auth
//	socks5://host:port        SOCKS5，域名在本地解析后按 IP 连接
//	socks5h://host:port        SOCKS5，域名交给代理解析（远端 DNS）
//	socks5://user:pass@host:port
//
// 空字符串表示直连（不经过代理）。
//
// 只依赖标准库：SOCKS5 握手自己实现（RFC 1928 / RFC 1929），
// 这样整个项目保持零第三方依赖。
package egress

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrUnsupportedScheme 表示 URL 的 scheme 不在支持列表里。
var ErrUnsupportedScheme = errors.New("egress: unsupported scheme (want http, https, socks5 or socks5h)")

// Proxy 是解析后的出口配置。
type Proxy struct {
	Raw       string // 原始 URL（去空格后）
	Scheme    string // http / https / socks5 / socks5h
	Host      string
	Port      string
	User      string
	Pass      string
	RemoteDNS bool // socks5h：域名交给代理解析
}

// Parse 解析出口 URL。空串返回 nil（表示直连）。
func Parse(raw string) (*Proxy, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, errors.New("egress: proxy URL is malformed")
	}
	scheme := strings.ToLower(u.Scheme)
	p := &Proxy{Raw: trimmed, Scheme: scheme}
	if u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("egress: proxy URL must contain only scheme, credentials, host and port")
	}
	switch scheme {
	case "http", "https":
	case "socks5", "socks5h":
		p.RemoteDNS = scheme == "socks5h"
	default:
		return nil, fmt.Errorf("%w（收到 %q）", ErrUnsupportedScheme, scheme)
	}
	host := u.Hostname()
	port := u.Port()
	if host == "" {
		return nil, fmt.Errorf("egress: %q 缺少主机名", Mask(trimmed))
	}
	if port == "" {
		switch scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			port = "1080"
		}
	}
	if n, err := strconv.Atoi(port); err != nil || n <= 0 || n > 65535 {
		return nil, fmt.Errorf("egress: %q 端口非法", Mask(trimmed))
	}
	p.Host, p.Port = host, port
	if u.User != nil {
		p.User = u.User.Username()
		p.Pass, _ = u.User.Password()
	}
	return p, nil
}

// IsSOCKS 判断是否为 SOCKS5 系。
func (p *Proxy) IsSOCKS() bool {
	return p != nil && (p.Scheme == "socks5" || p.Scheme == "socks5h")
}

// Addr 返回代理自身的 host:port。
func (p *Proxy) Addr() string {
	if p == nil {
		return ""
	}
	return net.JoinHostPort(p.Host, p.Port)
}

// Mask 把 URL 里的密码换成 ***，用于日志与接口回显。
func Mask(raw string) string {
	trimmed := strings.TrimSpace(raw)
	u, err := url.Parse(trimmed)
	if err != nil {
		return "[invalid proxy]"
	}
	if u.User == nil {
		return trimmed
	}
	if _, hasPass := u.User.Password(); !hasPass {
		return trimmed
	}
	// 手工拼回：url.String() 会把 *** 转义成 %2A%2A%2A，回显很难看。
	var b strings.Builder
	b.WriteString(u.Scheme)
	b.WriteString("://")
	b.WriteString(u.User.Username())
	b.WriteString(":***@")
	b.WriteString(u.Host)
	if path := u.EscapedPath(); path != "" && path != "/" {
		b.WriteString(path)
	}
	if u.RawQuery != "" {
		b.WriteString("?")
		b.WriteString(u.RawQuery)
	}
	return b.String()
}

// Transport 构造该出口对应的 http.Transport。base 提供超时等公共参数。
func (p *Proxy) Transport(base *http.Transport) (*http.Transport, error) {
	if p == nil {
		return base, nil
	}
	// 复制一份，避免多个出口共享同一个 Transport 的代理设置。
	transport := base.Clone()
	if !p.IsSOCKS() {
		u, err := url.Parse(p.Raw)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(u)
		return transport, nil
	}
	transport.Proxy = nil
	dial := p.DialContext
	transport.DialContext = dial
	transport.DialTLSContext = nil
	return transport, nil
}

// DialContext 通过 SOCKS5 代理建立连接。非 SOCKS 出口返回错误。
func (p *Proxy) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if !p.IsSOCKS() {
		return nil, fmt.Errorf("egress: DialContext 只支持 socks5/socks5h")
	}
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("egress: 不支持的网络 %q", network)
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", p.Addr())
	if err != nil {
		return nil, fmt.Errorf("egress: 连代理 %s 失败: %w", p.Addr(), err)
	}
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if err := p.handshake(ctx, conn, network, addr); err != nil {
		_ = conn.Close()
		return nil, err
	}
	// 握手完成后清掉 deadline，后续读写交给调用方（流式响应可能很长）。
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func (p *Proxy) handshake(ctx context.Context, conn net.Conn, network, addr string) error {
	// 1) 问候：声明支持的认证方式。
	methods := []byte{0x00} // 无认证
	if p.User != "" {
		methods = append(methods, 0x02) // 用户名/密码
	}
	greeting := []byte{0x05, byte(len(methods))}
	greeting = append(greeting, methods...)
	if _, err := conn.Write(greeting); err != nil {
		return fmt.Errorf("egress: socks 问候失败: %w", err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return fmt.Errorf("egress: socks 问候响应失败: %w", err)
	}
	if reply[0] != 0x05 {
		return fmt.Errorf("egress: socks 版本不符: 0x%02x", reply[0])
	}
	switch reply[1] {
	case 0x00:
	case 0x02:
		if p.User == "" {
			return errors.New("egress: 代理要求用户名密码，但 URL 里没有")
		}
		if err := p.auth(conn); err != nil {
			return err
		}
	case 0xFF:
		return errors.New("egress: 代理不接受任何认证方式")
	default:
		return fmt.Errorf("egress: 代理选择了不支持的认证方式 0x%02x", reply[1])
	}

	// 2) CONNECT 请求。
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("egress: 目标地址非法 %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		return fmt.Errorf("egress: 目标端口非法 %q", portStr)
	}
	req := []byte{0x05, 0x01, 0x00} // VER, CMD=CONNECT, RSV
	ip := net.ParseIP(host)
	switch {
	case ip != nil && ip.To4() != nil:
		req = append(req, 0x01)
		req = append(req, ip.To4()...)
	case ip != nil:
		req = append(req, 0x04)
		req = append(req, ip.To16()...)
	default:
		if !p.RemoteDNS {
			resolved, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil || len(resolved) == 0 {
				return fmt.Errorf("egress: 本地解析 %q 失败: %w", host, err)
			}
			ip = net.ParseIP(resolved[0])
			if ip == nil {
				return fmt.Errorf("egress: 解析 %q 得到非法地址", host)
			}
			if ip.To4() != nil {
				req = append(req, 0x01)
				req = append(req, ip.To4()...)
			} else {
				req = append(req, 0x04)
				req = append(req, ip.To16()...)
			}
		} else {
			if len(host) > 255 {
				return fmt.Errorf("egress: 域名过长 %q", host)
			}
			req = append(req, 0x03, byte(len(host)))
			req = append(req, []byte(host)...)
		}
	}
	req = append(req, byte(port>>8), byte(port&0xff))
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("egress: socks CONNECT 发送失败: %w", err)
	}

	// 3) 响应。
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return fmt.Errorf("egress: socks CONNECT 响应失败: %w", err)
	}
	if head[0] != 0x05 {
		return fmt.Errorf("egress: socks 响应版本不符: 0x%02x", head[0])
	}
	if head[1] != 0x00 {
		return fmt.Errorf("egress: socks 连接被拒绝: %s", replyMessage(head[1]))
	}
	var skip int
	switch head[3] {
	case 0x01:
		skip = 4
	case 0x04:
		skip = 16
	case 0x03:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return fmt.Errorf("egress: socks 读取域名长度失败: %w", err)
		}
		skip = int(length[0])
	default:
		return fmt.Errorf("egress: socks 响应地址类型未知: 0x%02x", head[3])
	}
	if _, err := io.ReadFull(conn, make([]byte, skip+2)); err != nil {
		return fmt.Errorf("egress: socks 读取绑定地址失败: %w", err)
	}
	return nil
}

func (p *Proxy) auth(conn net.Conn) error {
	if len(p.User) > 255 || len(p.Pass) > 255 {
		return errors.New("egress: socks 用户名或密码过长")
	}
	buf := []byte{0x01, byte(len(p.User))}
	buf = append(buf, []byte(p.User)...)
	buf = append(buf, byte(len(p.Pass)))
	buf = append(buf, []byte(p.Pass)...)
	if _, err := conn.Write(buf); err != nil {
		return fmt.Errorf("egress: socks 认证发送失败: %w", err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return fmt.Errorf("egress: socks 认证响应失败: %w", err)
	}
	if reply[1] != 0x00 {
		return errors.New("egress: socks 用户名或密码错误")
	}
	return nil
}

func replyMessage(code byte) string {
	switch code {
	case 0x01:
		return "general SOCKS server failure"
	case 0x02:
		return "connection not allowed by ruleset"
	case 0x03:
		return "network unreachable"
	case 0x04:
		return "host unreachable"
	case 0x05:
		return "connection refused"
	case 0x06:
		return "TTL expired"
	case 0x07:
		return "command not supported"
	case 0x08:
		return "address type not supported"
	default:
		return fmt.Sprintf("unknown reply 0x%02x", code)
	}
}

// Port 便于测试与展示。
func (p *Proxy) PortNum() int {
	if p == nil {
		return 0
	}
	n, _ := strconv.Atoi(p.Port)
	return n
}

// 保底：部分平台下 binary 包只用于端口编码，这里显式引用避免误删。
var _ = binary.BigEndian
