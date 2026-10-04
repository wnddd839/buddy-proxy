package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

const (
	imageGenerationsPath = "/v2/images/generations"
	imageEditsPath       = "/v2/images/edits"
)

// ImageRequest is the upstream body for POST /v2/images/generations and /v2/images/edits.
// Field names match what the live WorkBuddy endpoint accepts:
// image_url and image_data are []string; a bare string is rejected with 11101.
// n and response_format are not forwarded: live calls ignore both and always return one url.
type ImageRequest struct {
	Model     string
	Prompt    string
	ImageURLs []string
	ImageData []string
}

// ImageResult is one successful upstream image response.
type ImageResult struct {
	Created int64
	URLs    []string
	Usage   Usage
	Credit  float64
}

// Image sends a text-to-image or image-edit request.
// Edit is selected when ImageURLs or ImageData is non-empty.
func (c *Client) Image(ctx context.Context, opts ChatOptions, req ImageRequest) (ImageResult, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(opts.Model)
	}
	body := map[string]any{
		"model":  model,
		"prompt": req.Prompt,
	}
	path := imageGenerationsPath
	if len(req.ImageURLs) > 0 || len(req.ImageData) > 0 {
		path = imageEditsPath
		if len(req.ImageURLs) > 0 {
			body["image_url"] = req.ImageURLs
		}
		if len(req.ImageData) > 0 {
			body["image_data"] = req.ImageData
		}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return ImageResult{}, err
	}
	endpoint := ResolveProtocolDirectImageEndpoint(opts, path)
	domain := ResolveProtocolDirectDomain(opts)
	region := RegionOf(opts)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return ImageResult{}, err
	}
	headers, _ := c.BuildProtocolDirectHeaders(opts)
	headers.Set("Accept", "application/json, text/event-stream")
	httpReq.Header = headers

	httpClient := c.httpClient()
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return ImageResult{}, fmt.Errorf("CodeBuddy image request transport error: %w [region=%s site=%s endpoint=%s domain=%s model=%s]", err, region, config.NormalizeSite(opts.Site), endpoint, domain, model)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return ImageResult{}, fmt.Errorf("CodeBuddy image request read error: %w [region=%s site=%s endpoint=%s model=%s]", err, region, config.NormalizeSite(opts.Site), endpoint, model)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := extractErrorMessage(raw, resp.StatusCode)
		return ImageResult{}, &ChatError{
			Status:     resp.StatusCode,
			RetryAfter: resp.Header.Get("Retry-After"),
			Msg:        fmt.Sprintf("CodeBuddy image request failed with %d: %s [region=%s site=%s endpoint=%s domain=%s model=%s]", resp.StatusCode, msg, region, config.NormalizeSite(opts.Site), endpoint, domain, model),
		}
	}
	parsed, err := parseImageResponse(raw)
	if err != nil {
		return ImageResult{}, fmt.Errorf("CodeBuddy image request failed: %s [region=%s site=%s endpoint=%s model=%s]", err.Error(), region, config.NormalizeSite(opts.Site), endpoint, model)
	}
	return parsed, nil
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// ResolveProtocolDirectImageEndpoint builds a /v2/images/* URL with the same region rules as chat.
func ResolveProtocolDirectImageEndpoint(opts ChatOptions, imagePath string) string {
	imagePath = strings.TrimSpace(imagePath)
	if imagePath == "" {
		imagePath = imageGenerationsPath
	}
	if !strings.HasPrefix(imagePath, "/") {
		imagePath = "/" + imagePath
	}
	base := strings.TrimRight(ResolveProtocolDirectBaseURL(opts), "/")
	region := RegionOf(opts)
	if endpoint := strings.TrimRight(strings.TrimSpace(opts.APIEndpoint), "/"); endpoint != "" && endpointMatchesRegion(endpoint, region) {
		base = strings.TrimRight(protocolDirectHostBase(endpoint), "/")
	}
	if strings.HasSuffix(base, "/v2") && strings.HasPrefix(imagePath, "/v2/") {
		imagePath = imagePath[3:]
	}
	return base + imagePath
}

type imageEnvelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data imageDataObject `json:"data"`
}

type imageDataObject struct {
	Created int64            `json:"created"`
	Data    []imageURLObject `json:"data"`
	Usage   map[string]any   `json:"usage"`
}

type imageURLObject struct {
	URL string `json:"url"`
}

func parseImageResponse(raw []byte) (ImageResult, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return ImageResult{}, fmt.Errorf("empty image response")
	}
	// Content-Type is text/event-stream, but a successful body is one JSON object, not SSE frames.
	if rest, ok := strings.CutPrefix(text, "data:"); ok {
		line, _, _ := strings.Cut(strings.TrimSpace(rest), "\n")
		text = strings.TrimSpace(line)
	}
	var env imageEnvelope
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		return ImageResult{}, fmt.Errorf("invalid image response: %w", err)
	}
	if env.Code != 0 {
		msg := strings.TrimSpace(env.Msg)
		if msg == "" {
			msg = "upstream image error"
		}
		return ImageResult{}, fmt.Errorf("%s (code %d)", msg, env.Code)
	}
	urls := make([]string, 0, len(env.Data.Data))
	for _, item := range env.Data.Data {
		if url := strings.TrimSpace(item.URL); url != "" {
			urls = append(urls, url)
		}
	}
	if len(urls) == 0 {
		return ImageResult{}, fmt.Errorf("image response contained no url")
	}
	usage := ParseUsage(env.Data.Usage)
	credit := 0.0
	if usage.Credit != nil {
		credit = *usage.Credit
	}
	return ImageResult{
		Created: env.Data.Created,
		URLs:    urls,
		Usage:   usage,
		Credit:  credit,
	}, nil
}

// CreatedUnix returns the upstream timestamp, or now when the payload omitted it.
func (r ImageResult) CreatedUnix() int64 {
	if r.Created > 0 {
		return r.Created
	}
	return time.Now().Unix()
}
