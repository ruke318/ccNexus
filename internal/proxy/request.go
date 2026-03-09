package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lich0821/ccNexus/internal/codexpool"
	"golang.org/x/net/proxy"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/transformer"
	"github.com/lich0821/ccNexus/internal/transformer/cc"
	"github.com/lich0821/ccNexus/internal/transformer/cx/chat"
	"github.com/lich0821/ccNexus/internal/transformer/cx/responses"
)

const (
	codexPoolBackendBaseURL     = "https://chatgpt.com/backend-api/codex"
	codexPoolDefaultInstruction = "You are Codex. Be concise."
)

// prepareTransformerForClient creates transformer based on client format and endpoint
func prepareTransformerForClient(clientFormat ClientFormat, endpoint config.Endpoint) (transformer.Transformer, error) {
	endpointTransformer := endpoint.Transformer
	if endpointTransformer == "" {
		endpointTransformer = "claude"
	}

	switch clientFormat {
	case ClientFormatClaude:
		return prepareCCTransformer(endpoint, endpointTransformer)
	case ClientFormatOpenAIChat:
		return prepareCxChatTransformer(endpoint, endpointTransformer)
	case ClientFormatOpenAIResponses:
		return prepareCxRespTransformer(endpoint, endpointTransformer)
	}

	return nil, fmt.Errorf("unsupported client format: %s", clientFormat)
}

// prepareCCTransformer creates transformer for Claude Code client
func prepareCCTransformer(endpoint config.Endpoint, endpointTransformer string) (transformer.Transformer, error) {
	switch endpointTransformer {
	case "claude":
		if endpoint.Model != "" {
			logger.Debug("[%s] Using cc_claude with model override: %s", endpoint.Name, endpoint.Model)
			return cc.NewClaudeTransformerWithModel(endpoint.Model), nil
		}
		return cc.NewClaudeTransformer(), nil
	case "openai":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("OpenAI transformer requires model field")
		}
		return cc.NewOpenAITransformer(endpoint.Model), nil
	case "openai2":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("OpenAI2 transformer requires model field")
		}
		return cc.NewOpenAI2Transformer(endpoint.Model), nil
	case "gemini":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("Gemini transformer requires model field")
		}
		return cc.NewGeminiTransformer(endpoint.Model), nil
	default:
		return nil, fmt.Errorf("unsupported endpoint transformer: %s", endpointTransformer)
	}
}

// prepareCxChatTransformer creates transformer for Codex Chat API client
func prepareCxChatTransformer(endpoint config.Endpoint, endpointTransformer string) (transformer.Transformer, error) {
	switch endpointTransformer {
	case "claude":
		model := endpoint.Model
		if model == "" {
			model = "claude-sonnet-4-20250514"
		}
		return chat.NewClaudeTransformer(model), nil
	case "openai":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("OpenAI transformer requires model field")
		}
		return chat.NewOpenAITransformer(endpoint.Model), nil
	case "openai2":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("OpenAI2 transformer requires model field")
		}
		return chat.NewOpenAI2Transformer(endpoint.Model), nil
	case "gemini":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("Gemini transformer requires model field")
		}
		return chat.NewGeminiTransformer(endpoint.Model), nil
	default:
		return nil, fmt.Errorf("unsupported endpoint transformer for Codex Chat: %s", endpointTransformer)
	}
}

// prepareCxRespTransformer creates transformer for Codex Responses API client
func prepareCxRespTransformer(endpoint config.Endpoint, endpointTransformer string) (transformer.Transformer, error) {
	switch endpointTransformer {
	case "claude":
		model := endpoint.Model
		if model == "" {
			model = "claude-sonnet-4-20250514"
		}
		return responses.NewClaudeTransformer(model), nil
	case "openai":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("OpenAI transformer requires model field")
		}
		return responses.NewOpenAITransformer(endpoint.Model), nil
	case "openai2":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("OpenAI2 transformer requires model field")
		}
		return responses.NewOpenAI2Transformer(endpoint.Model), nil
	case "gemini":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("Gemini transformer requires model field")
		}
		return responses.NewGeminiTransformer(endpoint.Model), nil
	default:
		return nil, fmt.Errorf("unsupported endpoint transformer for Codex Responses: %s", endpointTransformer)
	}
}

// getTargetPath determines the target API path based on transformer name
func getTargetPath(originalPath string, endpoint config.Endpoint, transformedBody []byte, transformerName string) string {
	if useCodexPoolResponsesBackend(endpoint, transformerName) {
		return "/responses"
	}

	switch transformerName {
	case "cc_claude", "cx_chat_claude", "cx_resp_claude":
		return "/v1/messages"
	case "cc_openai", "cx_chat_openai", "cx_resp_openai":
		return "/v1/chat/completions"
	case "cc_openai2", "cx_resp_openai2", "cx_chat_openai2":
		return "/v1/responses"
	case "cc_gemini", "cx_chat_gemini", "cx_resp_gemini":
		var geminiReq struct {
			Stream bool `json:"stream"`
		}
		json.Unmarshal(transformedBody, &geminiReq)
		if geminiReq.Stream {
			return fmt.Sprintf("/v1beta/models/%s:streamGenerateContent", endpoint.Model)
		}
		return fmt.Sprintf("/v1beta/models/%s:generateContent", endpoint.Model)
	}
	return originalPath
}

// buildProxyRequest creates an HTTP request for the target API
func buildProxyRequest(r *http.Request, endpoint config.Endpoint, transformedBody []byte, transformerName string, authCtx *codexpool.AuthContext) (*http.Request, error) {
	if endpoint.AuthType == codexpool.AuthTypeCodexPool && !useCodexPoolResponsesBackend(endpoint, transformerName) {
		return nil, fmt.Errorf("codex pool currently only supports OpenAI2 (Responses API) transformer")
	}

	if useCodexPoolResponsesBackend(endpoint, transformerName) {
		var err error
		transformedBody, err = prepareCodexPoolRequestBody(transformedBody)
		if err != nil {
			return nil, err
		}
	}

	targetPath := getTargetPath(r.URL.Path, endpoint, transformedBody, transformerName)
	if targetPath == "" {
		targetPath = r.URL.Path
	}

	normalizedAPIURL := normalizeUpstreamBaseURL(endpoint, transformerName)
	targetURL := fmt.Sprintf("%s%s", strings.TrimRight(normalizedAPIURL, "/"), targetPath)
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(transformedBody))
	if err != nil {
		return nil, err
	}

	// Copy headers (except Host and Accept-Encoding)
	for key, values := range r.Header {
		if key == "Host" || key == "Accept-Encoding" {
			continue
		}
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// Force gzip or no compression to avoid unsupported encodings (e.g., brotli)
	proxyReq.Header.Set("Accept-Encoding", "gzip, identity")

	// Set authentication based on transformer type
	switch transformerName {
	case "cc_openai", "cc_openai2", "cx_chat_openai", "cx_chat_openai2", "cx_resp_openai", "cx_resp_openai2":
		bearerToken := endpoint.APIKey
		if authCtx != nil && strings.TrimSpace(authCtx.BearerToken) != "" {
			bearerToken = authCtx.BearerToken
		}
		proxyReq.Header.Set("Authorization", "Bearer "+bearerToken)
		if authCtx != nil && strings.TrimSpace(authCtx.AccountID) != "" {
			proxyReq.Header.Set("ChatGPT-Account-Id", authCtx.AccountID)
		}
		if useCodexPoolResponsesBackend(endpoint, transformerName) {
			proxyReq.Header.Set("Accept", "text/event-stream")
		}
	case "cc_gemini", "cx_chat_gemini", "cx_resp_gemini":
		q := proxyReq.URL.Query()
		q.Set("key", endpoint.APIKey)
		q.Set("alt", "sse")
		proxyReq.URL.RawQuery = q.Encode()
	default:
		// Claude endpoints
		proxyReq.Header.Set("x-api-key", endpoint.APIKey)
		proxyReq.Header.Set("Authorization", "Bearer "+endpoint.APIKey)
	}

	// net/http uses Request.Host for outbound Host header overrides.
	proxyReq.Host = proxyReq.URL.Host

	return proxyReq, nil
}

func useCodexPoolResponsesBackend(endpoint config.Endpoint, transformerName string) bool {
	if endpoint.AuthType != codexpool.AuthTypeCodexPool {
		return false
	}

	switch transformerName {
	case "cc_openai2", "cx_chat_openai2", "cx_resp_openai2":
		return true
	default:
		return false
	}
}

func normalizeUpstreamBaseURL(endpoint config.Endpoint, transformerName string) string {
	if useCodexPoolResponsesBackend(endpoint, transformerName) {
		return codexPoolBackendBaseURL
	}

	normalizedAPIURL := normalizeAPIUrl(endpoint.APIUrl)
	if !strings.HasPrefix(normalizedAPIURL, "http://") && !strings.HasPrefix(normalizedAPIURL, "https://") {
		normalizedAPIURL = "https://" + normalizedAPIURL
	}
	return normalizedAPIURL
}

func prepareCodexPoolRequestBody(transformedBody []byte) ([]byte, error) {
	if len(transformedBody) == 0 {
		return nil, fmt.Errorf("codex pool request body is empty")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(transformedBody, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse codex pool request body: %w", err)
	}

	payload["store"] = false
	payload["stream"] = true

	if instructions, ok := payload["instructions"].(string); !ok || strings.TrimSpace(instructions) == "" {
		payload["instructions"] = codexPoolDefaultInstruction
	}

	if _, ok := payload["input"]; !ok {
		return nil, fmt.Errorf("codex pool request body is missing input")
	}

	normalizedBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode codex pool request body: %w", err)
	}
	return normalizedBody, nil
}

// sendRequest sends the HTTP request and returns the response
func sendRequest(ctx context.Context, proxyReq *http.Request, endpoint config.Endpoint, cfg *config.Config) (*http.Response, error) {
	proxyReq = proxyReq.WithContext(ctx)
	client := &http.Client{
		Timeout: 300 * time.Second,
	}

	// Apply proxy if configured
	proxyURL := endpoint.ProxyURL
	if proxyURL == "" {
		if proxyCfg := cfg.GetProxy(); proxyCfg != nil {
			proxyURL = proxyCfg.URL
		}
	}
	if proxyURL != "" {
		transport, err := CreateProxyTransport(proxyURL)
		if err != nil {
			logger.Warn("Failed to create proxy transport: %v, using direct connection", err)
		} else {
			client.Transport = transport
			logger.Debug("Using proxy: %s", proxyURL)
		}
	}

	return client.Do(proxyReq)
}

// CreateProxyTransport creates an http.Transport with proxy support
func CreateProxyTransport(proxyURL string) (*http.Transport, error) {
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}

	transport := &http.Transport{}

	switch parsed.Scheme {
	case "socks5", "socks5h":
		auth := &proxy.Auth{}
		if parsed.User != nil {
			auth.User = parsed.User.Username()
			auth.Password, _ = parsed.User.Password()
		} else {
			auth = nil
		}
		dialer, err := proxy.SOCKS5("tcp", parsed.Host, auth, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
		}
		transport.Dial = dialer.Dial
	case "http", "https":
		transport.Proxy = http.ProxyURL(parsed)
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", parsed.Scheme)
	}

	return transport, nil
}
