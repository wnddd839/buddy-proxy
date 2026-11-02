package watch

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

// WebhookNotifier 把事件 POST 成 JSON。URL 为空时静默丢弃（仅本地日志）。
type WebhookNotifier struct {
	url    string
	token  string
	client *http.Client
}

// NewWebhookNotifier 构造 webhook 投递器。
func NewWebhookNotifier(url, token string, timeout time.Duration) *WebhookNotifier {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &WebhookNotifier{url: url, token: token, client: &http.Client{Timeout: timeout}}
}

// Notify 实现 Notifier。
func (w *WebhookNotifier) Notify(ctx context.Context, ev Event) error {
	if w == nil || w.url == "" {
		return nil
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.token != "" {
		req.Header.Set("Authorization", "Bearer "+w.token)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook 返回 %s", resp.Status)
	}
	return nil
}

// SMTPConfig 是邮件告警配置。
type SMTPConfig struct {
	Addr    string // host:port
	User    string
	Pass    string
	From    string
	To      []string
	Mode    string // implicit | starttls | none；空则按端口推断
	Timeout time.Duration
}

// Enabled 报告是否具备发信条件。
func (c SMTPConfig) Enabled() bool {
	return strings.TrimSpace(c.Addr) != "" && strings.TrimSpace(c.From) != "" && len(c.To) > 0
}

// SMTPNotifier 通过 SMTP 发告警邮件。
type SMTPNotifier struct {
	cfg      SMTPConfig
	adminURL string
}

// NewSMTPNotifier 构造邮件投递器。
func NewSMTPNotifier(cfg SMTPConfig) *SMTPNotifier {
	return newSMTPNotifier(cfg, "")
}

func newSMTPNotifier(cfg SMTPConfig, adminURL string) *SMTPNotifier {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	if cfg.Mode == "" {
		if _, port, err := net.SplitHostPort(cfg.Addr); err == nil && port == "465" {
			cfg.Mode = "implicit"
		} else {
			cfg.Mode = "starttls"
		}
	}
	return &SMTPNotifier{cfg: cfg, adminURL: adminURL}
}

// Notify 实现 Notifier。
func (s *SMTPNotifier) Notify(ctx context.Context, ev Event) error {
	if s == nil || !s.cfg.Enabled() {
		return nil
	}
	subject, body := renderMail(ev, s.adminURL)
	msg := buildMessage(s.cfg.From, s.cfg.To, subject, body)

	host, _, err := net.SplitHostPort(s.cfg.Addr)
	if err != nil {
		host = s.cfg.Addr
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	rawConn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	defer rawConn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := rawConn.SetDeadline(deadline); err != nil {
			return err
		}
	}
	stop := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	defer stop()
	var conn net.Conn = rawConn
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	mode := strings.ToLower(s.cfg.Mode)
	if mode == "implicit" || mode == "tls" {
		secured := tls.Client(conn, tlsConfig)
		if err := secured.HandshakeContext(ctx); err != nil {
			return err
		}
		conn = secured
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if mode != "implicit" && mode != "tls" && mode != "none" && mode != "plain" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("smtp 服务器不支持 STARTTLS；如需直连请设置 mode=none")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}

	if s.cfg.User != "" {
		if err := client.Auth(smtp.PlainAuth("", s.cfg.User, s.cfg.Pass, host)); err != nil {
			return fmt.Errorf("smtp 认证失败: %w", err)
		}
	}
	if err := client.Mail(s.cfg.From); err != nil {
		return err
	}
	for _, rcpt := range s.cfg.To {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// MultiNotifier 把事件广播给多个通道，任一失败即返回错误（不影响其他通道）。
type MultiNotifier []Notifier

// Notify 实现 Notifier。
func (m MultiNotifier) Notify(ctx context.Context, ev Event) error {
	var errs []error
	for _, n := range m {
		if n == nil {
			continue
		}
		if err := n.Notify(ctx, ev); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// NewNotifier 依据配置组装投递链；未配置任何通道时返回 nil（只写日志）。
func NewNotifier(cfg Config) Notifier {
	var chain MultiNotifier
	if strings.TrimSpace(cfg.WebhookURL) != "" {
		chain = append(chain, NewWebhookNotifier(cfg.WebhookURL, cfg.WebhookToken, cfg.WebhookTimeout))
	}
	if cfg.SMTP.Enabled() {
		chain = append(chain, newSMTPNotifier(cfg.SMTP, cfg.AdminURL))
	}
	if len(chain) == 0 {
		return nil
	}
	return chain
}

func renderMail(ev Event, adminURL string) (subject, body string) {
	tag := "告警"
	switch ev.Kind {
	case "recover":
		tag = "恢复"
	case "remind":
		tag = "持续"
	}
	subject = fmt.Sprintf("[%s] CodeBuddy Proxy %s", tag, ev.Reason)
	at := time.UnixMilli(ev.At).Format("2006-01-02 15:04:05")
	var b strings.Builder
	fmt.Fprintf(&b, "时间：%s\n类型：%s\n原因：%s\n", at, ev.Kind, ev.Reason)
	if ev.Since > 0 {
		fmt.Fprintf(&b, "起始：%s\n持续：%d 分钟\n",
			time.UnixMilli(ev.Since).Format("2006-01-02 15:04:05"),
			int64(time.UnixMilli(ev.At).Sub(time.UnixMilli(ev.Since)).Minutes()))
	}
	b.WriteString("\n指标：\n")
	fmt.Fprintf(&b, "  样本数        %d\n", ev.Metrics.Samples)
	fmt.Fprintf(&b, "  错误率        %.1f%%\n", ev.Metrics.ErrorRate*100)
	fmt.Fprintf(&b, "  推理缺失      %d/%d (%.1f%%，基线 %.1f%%)\n",
		ev.Metrics.ReasoningMissed, ev.Metrics.ReasoningAsked,
		ev.Metrics.ReasoningMissRate*100, ev.Metrics.BaselineMissRate*100)
	fmt.Fprintf(&b, "  耗时 P50      %dms（基线 %dms）\n", ev.Metrics.LatencyP50, ev.Metrics.BaselineLatencyP50)
	b.WriteString("\n排查：\n")
	if adminURL != "" {
		fmt.Fprintf(&b, "  1) 管理台概览页的「波动监测」条：%s\n", adminURL)
		b.WriteString("  2) 进程日志：journalctl -u <服务名> | grep 'watch:'\n")
	} else {
		b.WriteString("  1) 进程日志：journalctl -u <服务名> | grep 'watch:'\n")
	}
	b.WriteString("  3) 换一个问法手动打一发（不要复用同一句），看 reasoning_content 与 reasoning_tokens\n")
	b.WriteString("\n对号入座：\n")
	b.WriteString("  · 502 且上游返回 403 code 11140 → 上游把重复/相似请求判为 illegal，不是网关故障\n")
	b.WriteString("  · 200 但没有 reasoning → 上游降档或模型默认关闭思考\n")
	b.WriteString("  · 只有推理 token 下降、内容仍在 → 账号切换或降档，不是服务宕机\n")
	b.WriteString("\n判定依据：所有指标都相对本进程自身的滚动基线比较，不是固定阈值。\n")
	return subject, b.String()
}

func buildMessage(from string, to []string, subject, body string) []byte {
	var b bytes.Buffer
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.Bytes()
}
