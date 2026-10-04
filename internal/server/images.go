package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/gateway"
	"github.com/wnddd839/codebuddy-proxy/internal/httputil"
	"github.com/wnddd839/codebuddy-proxy/internal/openai"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

var (
	errImageInputOnGenerate = errors.New("image input belongs on POST /v1/images/edits")
	errImageInputRequired   = errors.New("edits require image (url or base64) or image_url")
)

type imageGenerateBody struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              *int   `json:"n"`
	Size           string `json:"size"`
	ResponseFormat string `json:"response_format"`
	Image          any    `json:"image"`
	ImageURL       any    `json:"image_url"`
}

func (s *Server) handleImagesAuth(w http.ResponseWriter, r *http.Request) {
	ok, keySite := s.authorizeAPI(w, r)
	if !ok {
		return
	}
	s.handleImages(w, r, keySite, false)
}

func (s *Server) handleImageEditsAuth(w http.ResponseWriter, r *http.Request) {
	ok, keySite := s.authorizeAPI(w, r)
	if !ok {
		return
	}
	s.handleImages(w, r, keySite, true)
}

func (s *Server) handleImages(w http.ResponseWriter, r *http.Request, keySite string, edit bool) {
	var body imageGenerateBody
	if err := httputil.ReadJSON(r, &body); err != nil {
		if errors.Is(err, httputil.ErrBodyTooLarge) {
			httputil.WriteJSON(w, http.StatusRequestEntityTooLarge, openai.NewError("Request body exceeds 64MB.", "invalid_request_error"))
			return
		}
		httputil.WriteJSON(w, http.StatusBadRequest, openai.NewError("Invalid JSON body", "invalid_request_error"))
		return
	}
	prompt := strings.TrimSpace(body.Prompt)
	if prompt == "" {
		httputil.WriteJSON(w, http.StatusBadRequest, openai.NewError("prompt is required", "invalid_request_error"))
		return
	}
	if body.N != nil && *body.N != 1 {
		httputil.WriteJSON(w, http.StatusBadRequest, openai.NewError("n must be 1; upstream image generation returns a single image", "invalid_request_error"))
		return
	}
	format := strings.ToLower(strings.TrimSpace(body.ResponseFormat))
	if format == "b64_json" {
		httputil.WriteJSON(w, http.StatusBadRequest, openai.NewError("response_format b64_json is not supported; upstream returns a url", "invalid_request_error"))
		return
	}
	urls, data, err := imageInputs(body, edit)
	if err != nil {
		httputil.WriteJSON(w, http.StatusBadRequest, openai.NewError(err.Error(), "invalid_request_error"))
		return
	}
	providerModel := gateway.ResolveProviderModel(body.Model)
	site := resolveRequestSite(providerModel.Site, r.Header.Get("X-Site"), keySite)
	completeOpts := gateway.CompleteOptions{Model: providerModel.Model, Site: site}
	started := time.Now()
	proxyRequestID := s.newProxyRequestID()
	finish := s.Svc.BeginRequest(providerModel.PublicModel, len(prompt), false)
	result, err := s.Svc.ImageFromPool(r.Context(), gateway.ImageOptions{
		Site:      site,
		Model:     providerModel.Model,
		Prompt:    prompt,
		ImageURLs: urls,
		ImageData: data,
	})
	if err != nil {
		if openai.IsClientCanceled(err) {
			s.recordChatJournal(started, proxyRequestID, completeOpts, providerModel.PublicModel, false, true, "", provider.Usage{}, nil)
			finish(true, 0, 0, 0, 0, "", provider.Usage{})
			return
		}
		s.recordChatJournal(started, proxyRequestID, completeOpts, providerModel.PublicModel, false, false, err.Error(), provider.Usage{}, nil)
		finish(false, 0, 0, 0, 0, err.Error(), provider.Usage{})
		s.writeChatError(w, http.StatusBadGateway, err)
		return
	}
	complete := gateway.CompleteResult{Account: result.Account, AccountID: result.AccountID}
	s.recordChatJournal(started, proxyRequestID, completeOpts, providerModel.PublicModel, false, true, "", result.Usage, &complete)
	finish(true, 0, 0, 0, 0, "", result.Usage)
	items := make([]map[string]any, 0, len(result.URLs))
	for _, url := range result.URLs {
		items = append(items, map[string]any{"url": url})
	}
	payload := map[string]any{
		"created": result.CreatedUnix(),
		"data":    items,
	}
	if result.Usage.TotalTokens > 0 || result.Usage.PromptTokens > 0 || result.Usage.CompletionTokens > 0 {
		payload["usage"] = openai.UsageFromProvider(result.Usage)
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	httputil.WriteJSON(w, http.StatusOK, payload)
}

func imageInputs(body imageGenerateBody, edit bool) (urls, data []string, err error) {
	urls = collectImageURLs(body.ImageURL)
	moreURLs, moreData := collectImageValues(body.Image)
	urls = append(urls, moreURLs...)
	data = append(data, moreData...)
	if !edit {
		if len(urls) > 0 || len(data) > 0 {
			return nil, nil, errImageInputOnGenerate
		}
		return nil, nil, nil
	}
	if len(urls) == 0 && len(data) == 0 {
		return nil, nil, errImageInputRequired
	}
	return urls, data, nil
}

func collectImageURLs(value any) []string {
	switch v := value.(type) {
	case string:
		if s := strings.TrimSpace(v); s != "" {
			return []string{s}
		}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			switch part := item.(type) {
			case string:
				if s := strings.TrimSpace(part); s != "" {
					out = append(out, s)
				}
			case map[string]any:
				if raw, ok := part["url"].(string); ok {
					if s := strings.TrimSpace(raw); s != "" {
						out = append(out, s)
					}
				}
			}
		}
		return out
	case map[string]any:
		if raw, ok := v["url"].(string); ok {
			if s := strings.TrimSpace(raw); s != "" {
				return []string{s}
			}
		}
	}
	return nil
}

func collectImageValues(value any) (urls, data []string) {
	switch v := value.(type) {
	case string:
		appendImageRef(&urls, &data, v)
	case []any:
		for _, item := range v {
			switch part := item.(type) {
			case string:
				appendImageRef(&urls, &data, part)
			case map[string]any:
				appendImageObject(&urls, &data, part)
			}
		}
	case map[string]any:
		appendImageObject(&urls, &data, v)
	}
	return urls, data
}

func appendImageObject(urls, data *[]string, obj map[string]any) {
	if raw, ok := obj["url"].(string); ok {
		appendImageRef(urls, data, raw)
	}
	if b64, ok := obj["b64_json"].(string); ok {
		appendImageRef(urls, data, b64)
	}
}

func appendImageRef(urls, data *[]string, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		*urls = append(*urls, raw)
	case strings.HasPrefix(lower, "data:"):
		_, rest, ok := strings.Cut(raw, ",")
		if ok && strings.TrimSpace(rest) != "" {
			*data = append(*data, rest)
		}
	default:
		*data = append(*data, raw)
	}
}
