package codexpool

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/storage"
)

const (
	AuthTypeAPIKey    = "apikey"
	AuthTypeCodexPool = "codex_pool"

	SlotStatusUnknown     = "unknown"
	SlotStatusReady       = "ready"
	SlotStatusCooling     = "cooling"
	SlotStatusAuthExpired = "auth_expired"
	SlotStatusDisabled    = "disabled"
	SlotStatusNotLoggedIn = "not_logged_in"

	defaultRefreshClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	defaultAuthorizeURL    = "https://auth.openai.com/oauth/authorize"
	defaultRefreshURL      = "https://auth.openai.com/oauth/token"
	defaultAuthScope       = "openid profile email offline_access"
	defaultCallbackPath    = "/auth/callback"
	defaultCallbackHost    = "localhost"
	defaultCallbackPort    = 1455
	defaultAuthTimeout     = 15 * time.Minute
)

type AuthContext struct {
	SlotID      int64
	SlotName    string
	BearerToken string
	AccountID   string
}

type Manager struct {
	storage      *storage.SQLiteStorage
	mu           sync.Mutex
	cursorByPool map[int64]int
	stateRoot    string
	activeAuth   *activeAuthSession
}

type activeAuthSession struct {
	slotID        int64
	slotName      string
	server        *http.Server
	cancelTimeout context.CancelFunc
}

type authFile struct {
	AuthMode string  `json:"auth_mode"`
	APIKey   *string `json:"OPENAI_API_KEY"`
	Tokens   struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
	LastRefresh string `json:"last_refresh"`
}

type refreshResponse struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type authorizationCodeResponse struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
}

func NewManager(s *storage.SQLiteStorage) *Manager {
	return &Manager{
		storage:      s,
		cursorByPool: make(map[int64]int),
		stateRoot:    defaultStateRoot(),
	}
}

func defaultStateRoot() string {
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		return filepath.Join(".", ".ccNexus", "codex-slots")
	}
	return filepath.Join(homeDir, ".ccNexus", "codex-slots")
}

func (m *Manager) ListSlots() ([]storage.CodexSlot, error) {
	if m.storage == nil {
		return nil, fmt.Errorf("storage not initialized")
	}
	return m.storage.GetCodexSlots()
}

func (m *Manager) ListPools() ([]storage.CodexPool, error) {
	if m.storage == nil {
		return nil, fmt.Errorf("storage not initialized")
	}
	return m.storage.GetCodexPools()
}

func (m *Manager) GetPool(id int64) (*storage.CodexPool, error) {
	if m.storage == nil {
		return nil, fmt.Errorf("storage not initialized")
	}
	return m.storage.GetCodexPool(id)
}

func (m *Manager) CreateSlot(name, accountID, remark string) (*storage.CodexSlot, error) {
	if m.storage == nil {
		return nil, fmt.Errorf("storage not initialized")
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("slot name is required")
	}

	stateDir := filepath.Join(m.stateRoot, sanitizeName(name))
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create slot directory: %w", err)
	}

	slot := &storage.CodexSlot{
		Name:      name,
		StateDir:  stateDir,
		AccountID: strings.TrimSpace(accountID),
		Status:    SlotStatusUnknown,
		Enabled:   true,
		Remark:    remark,
	}
	if err := m.storage.SaveCodexSlot(slot); err != nil {
		return nil, err
	}
	return m.SyncSlotStatus(slot.ID)
}

func (m *Manager) UpdateSlot(id int64, name, accountID, remark string, enabled bool) (*storage.CodexSlot, error) {
	slot, err := m.storage.GetCodexSlot(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) != "" {
		slot.Name = name
	}
	slot.AccountID = strings.TrimSpace(accountID)
	slot.Remark = remark
	slot.Enabled = enabled
	if !enabled {
		slot.Status = SlotStatusDisabled
	}
	if err := m.storage.UpdateCodexSlot(slot); err != nil {
		return nil, err
	}
	if !enabled {
		return slot, nil
	}
	return m.SyncSlotStatus(id)
}

func (m *Manager) DeleteSlot(id int64) error {
	if m.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	return m.storage.DeleteCodexSlot(id)
}

func (m *Manager) CreatePool(name string, slotIDs []int64) (*storage.CodexPool, error) {
	if m.storage == nil {
		return nil, fmt.Errorf("storage not initialized")
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("pool name is required")
	}
	pool := &storage.CodexPool{
		Name:              name,
		Strategy:          "rr",
		Enabled:           true,
		Cooldown429Sec:    600,
		Cooldown5xxSec:    120,
		AuthExpiredPolicy: "skip",
		SlotIDs:           dedupeSlotIDs(slotIDs),
	}
	if err := m.storage.SaveCodexPool(pool); err != nil {
		return nil, err
	}
	return m.storage.GetCodexPool(pool.ID)
}

func (m *Manager) UpdatePool(id int64, name, strategy string, enabled bool, cooldown429Sec, cooldown5xxSec int, authExpiredPolicy string, slotIDs []int64) (*storage.CodexPool, error) {
	pool, err := m.storage.GetCodexPool(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) != "" {
		pool.Name = name
	}
	if strings.TrimSpace(strategy) != "" {
		pool.Strategy = strategy
	}
	pool.Enabled = enabled
	if cooldown429Sec > 0 {
		pool.Cooldown429Sec = cooldown429Sec
	}
	if cooldown5xxSec > 0 {
		pool.Cooldown5xxSec = cooldown5xxSec
	}
	if strings.TrimSpace(authExpiredPolicy) != "" {
		pool.AuthExpiredPolicy = authExpiredPolicy
	}
	pool.SlotIDs = dedupeSlotIDs(slotIDs)
	if err := m.storage.UpdateCodexPool(pool); err != nil {
		return nil, err
	}
	return m.storage.GetCodexPool(pool.ID)
}

func (m *Manager) DeletePool(id int64) error {
	if m.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	return m.storage.DeleteCodexPool(id)
}

func (m *Manager) StartSlotAuth(slotID int64, openBrowser func(string)) error {
	if m.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	if openBrowser == nil {
		return fmt.Errorf("browser opener is not configured")
	}

	slot, err := m.storage.GetCodexSlot(slotID)
	if err != nil {
		return err
	}
	if slot == nil {
		return fmt.Errorf("slot not found: %d", slotID)
	}
	if !slot.Enabled {
		return fmt.Errorf("slot %s is disabled", slot.Name)
	}
	if err := os.MkdirAll(slot.StateDir, 0o755); err != nil {
		return fmt.Errorf("failed to prepare slot dir: %w", err)
	}

	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		return fmt.Errorf("failed to generate code verifier: %w", err)
	}
	state, err := randomOAuthString(24)
	if err != nil {
		return fmt.Errorf("failed to generate oauth state: %w", err)
	}

	m.cancelActiveAuthSession("previous browser authorization was cancelled")

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", defaultCallbackPort))
	if err != nil {
		return fmt.Errorf("failed to start auth callback listener on localhost:%d: %w", defaultCallbackPort, err)
	}
	redirectURL := fmt.Sprintf("http://%s:%d%s", defaultCallbackHost, defaultCallbackPort, defaultCallbackPath)
	authURL := buildAuthorizeURL(redirectURL, state, codeChallengeS256(codeVerifier))

	var completeOnce sync.Once
	var callbackMu sync.Mutex
	callbackHandled := false
	server := &http.Server{}
	timeoutCtx, cancelTimeout := context.WithCancel(context.Background())
	m.setActiveAuthSession(&activeAuthSession{
		slotID:        slot.ID,
		slotName:      slot.Name,
		server:        server,
		cancelTimeout: cancelTimeout,
	})
	shutdownServer := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
	complete := func(authErr error) {
		completeOnce.Do(func() {
			cancelTimeout()
			m.clearActiveAuthSession(server)
			if authErr != nil {
				logger.Warn("[%s] browser auth failed: %v", slot.Name, authErr)
			}
			go shutdownServer()
		})
	}

	mux := http.NewServeMux()
	mux.HandleFunc(defaultCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("state") != state {
			msg := "invalid auth state"
			writeAuthCallbackResponse(w, http.StatusBadRequest, false, msg)
			_ = m.markSlotState(slot.ID, SlotStatusNotLoggedIn, time.Time{}, msg, false)
			complete(fmt.Errorf("%s", msg))
			return
		}

		if authErr := strings.TrimSpace(query.Get("error")); authErr != "" {
			msg := authErr
			if desc := strings.TrimSpace(query.Get("error_description")); desc != "" {
				msg = fmt.Sprintf("%s: %s", authErr, desc)
			}
			writeAuthCallbackResponse(w, http.StatusBadRequest, false, msg)
			_ = m.markSlotState(slot.ID, SlotStatusNotLoggedIn, time.Time{}, msg, false)
			complete(fmt.Errorf("%s", msg))
			return
		}

		code := strings.TrimSpace(query.Get("code"))
		if code == "" {
			msg := "oauth callback missing code"
			writeAuthCallbackResponse(w, http.StatusBadRequest, false, msg)
			_ = m.markSlotState(slot.ID, SlotStatusNotLoggedIn, time.Time{}, msg, false)
			complete(fmt.Errorf("%s", msg))
			return
		}

		callbackMu.Lock()
		if callbackHandled {
			callbackMu.Unlock()
			logger.Warn("[%s] duplicate oauth callback ignored", slot.Name)
			writeAuthCallbackResponse(w, http.StatusOK, true, "Authorization already received. You can close this tab.")
			return
		}
		callbackHandled = true
		callbackMu.Unlock()

		logger.Info("[%s] oauth callback received authorization code", slot.Name)

		tokenResp, err := exchangeAuthorizationCode(code, redirectURL, codeVerifier)
		if err != nil {
			writeAuthCallbackResponse(w, http.StatusBadGateway, false, err.Error())
			_ = m.markSlotState(slot.ID, SlotStatusNotLoggedIn, time.Time{}, err.Error(), false)
			complete(err)
			return
		}
		if err := m.persistSlotAuth(slot, tokenResp); err != nil {
			writeAuthCallbackResponse(w, http.StatusInternalServerError, false, err.Error())
			_ = m.markSlotState(slot.ID, SlotStatusNotLoggedIn, time.Time{}, err.Error(), false)
			complete(err)
			return
		}

		writeAuthCallbackResponse(w, http.StatusOK, true, slot.Name)
		complete(nil)
	})
	server.Handler = mux

	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			logger.Warn("[%s] auth callback server stopped unexpectedly: %v", slot.Name, serveErr)
		}
	}()

	go func() {
		select {
		case <-time.After(defaultAuthTimeout):
			complete(fmt.Errorf("timed out waiting for browser authorization"))
			_ = m.markSlotState(slot.ID, SlotStatusNotLoggedIn, time.Time{}, "timed out waiting for browser authorization", false)
		case <-timeoutCtx.Done():
		}
	}()

	_ = m.markSlotState(slot.ID, SlotStatusUnknown, time.Time{}, "waiting for browser authorization", false)
	openBrowser(authURL)
	logger.Info("[%s] browser auth started: %s", slot.Name, redirectURL)
	return nil
}

func (m *Manager) cancelActiveAuthSession(reason string) {
	m.mu.Lock()
	session := m.activeAuth
	m.activeAuth = nil
	m.mu.Unlock()

	if session == nil {
		return
	}
	if session.cancelTimeout != nil {
		session.cancelTimeout()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := session.server.Shutdown(ctx); err != nil && err != http.ErrServerClosed {
		logger.Warn("[%s] failed to shutdown previous auth callback server: %v", session.slotName, err)
	}
	if strings.TrimSpace(reason) != "" {
		_ = m.markSlotState(session.slotID, SlotStatusNotLoggedIn, time.Time{}, reason, false)
	}
}

func (m *Manager) setActiveAuthSession(session *activeAuthSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeAuth = session
}

func (m *Manager) clearActiveAuthSession(server *http.Server) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeAuth != nil && m.activeAuth.server == server {
		m.activeAuth = nil
	}
}

func (m *Manager) GetPoolSlotCount(poolID int64) int {
	pool, err := m.storage.GetCodexPool(poolID)
	if err != nil || pool == nil || !pool.Enabled {
		return 0
	}
	return len(pool.SlotIDs)
}

func (m *Manager) ResolveAuthForPool(poolID int64) (*AuthContext, error) {
	if m.storage == nil {
		return nil, fmt.Errorf("storage not initialized")
	}
	pool, err := m.storage.GetCodexPool(poolID)
	if err != nil {
		return nil, err
	}
	if pool == nil || !pool.Enabled {
		return nil, fmt.Errorf("codex pool %d is not enabled", poolID)
	}
	if len(pool.SlotIDs) == 0 {
		return nil, fmt.Errorf("codex pool %s has no slots", pool.Name)
	}

	slots := make([]storage.CodexSlot, 0, len(pool.SlotIDs))
	for _, slotID := range pool.SlotIDs {
		slot, err := m.storage.GetCodexSlot(slotID)
		if err != nil {
			logger.Warn("failed to load codex slot %d: %v", slotID, err)
			continue
		}
		if slot == nil {
			continue
		}
		if !slot.Enabled {
			continue
		}
		if slot.Status == SlotStatusDisabled {
			continue
		}
		if !slot.CooldownUntil.IsZero() && slot.CooldownUntil.After(time.Now()) {
			continue
		}
		slots = append(slots, *slot)
	}
	if len(slots) == 0 {
		return nil, fmt.Errorf("codex pool %s has no available slots", pool.Name)
	}

	m.mu.Lock()
	start := m.cursorByPool[poolID] % len(slots)
	m.mu.Unlock()

	var lastErr error
	for i := 0; i < len(slots); i++ {
		slot := slots[(start+i)%len(slots)]
		auth, err := m.resolveSlotAuth(&slot)
		if err != nil {
			lastErr = err
			logger.Warn("[%s] failed to resolve auth: %v", slot.Name, err)
			status := SlotStatusAuthExpired
			if os.IsNotExist(err) {
				status = SlotStatusNotLoggedIn
			}
			_ = m.markSlotState(slot.ID, status, time.Time{}, err.Error(), false)
			continue
		}

		m.mu.Lock()
		m.cursorByPool[poolID] = (start + i + 1) % len(slots)
		m.mu.Unlock()

		return auth, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no usable slots in pool %s", pool.Name)
	}
	return nil, lastErr
}

func (m *Manager) MarkSuccess(slotID int64) {
	_ = m.markSlotState(slotID, SlotStatusReady, time.Time{}, "", true)
}

func (m *Manager) MarkRateLimited(slotID int64, cooldown time.Duration, errMsg string) {
	_ = m.markSlotState(slotID, SlotStatusCooling, time.Now().Add(cooldown), errMsg, false)
}

func (m *Manager) MarkServerError(slotID int64, cooldown time.Duration, errMsg string) {
	_ = m.markSlotState(slotID, SlotStatusCooling, time.Now().Add(cooldown), errMsg, false)
}

func (m *Manager) MarkAuthExpired(slotID int64, errMsg string) {
	_ = m.markSlotState(slotID, SlotStatusAuthExpired, time.Time{}, errMsg, false)
}

func (m *Manager) SyncSlotStatus(slotID int64) (*storage.CodexSlot, error) {
	slot, err := m.storage.GetCodexSlot(slotID)
	if err != nil {
		return nil, err
	}
	if slot == nil {
		return nil, fmt.Errorf("slot not found: %d", slotID)
	}
	if !slot.Enabled {
		slot.Status = SlotStatusDisabled
		if err := m.storage.UpdateCodexSlot(slot); err != nil {
			return nil, err
		}
		return slot, nil
	}

	auth, err := loadAuthFile(slot.StateDir)
	if err != nil {
		slot.Status = SlotStatusNotLoggedIn
		slot.LastError = ""
		slot.LastCheckedAt = time.Now()
		if err := m.storage.UpdateCodexSlot(slot); err != nil {
			return nil, err
		}
		return slot, nil
	}

	slot.Status = SlotStatusReady
	if exp, ok := extractExpiry(auth.Tokens.AccessToken); ok && exp.Before(time.Now()) {
		slot.Status = SlotStatusAuthExpired
		slot.LastError = "access token expired"
	} else {
		slot.LastError = ""
	}
	slot.LastCheckedAt = time.Now()
	if slot.AccountID == "" {
		slot.AccountID = strings.TrimSpace(auth.Tokens.AccountID)
	}
	if err := m.storage.UpdateCodexSlot(slot); err != nil {
		return nil, err
	}
	return slot, nil
}

func (m *Manager) resolveSlotAuth(slot *storage.CodexSlot) (*AuthContext, error) {
	auth, err := loadAuthFile(slot.StateDir)
	if err != nil {
		return nil, err
	}
	if needsRefresh(auth) {
		if err := refreshAuthFile(slot.StateDir, auth); err != nil {
			logger.Warn("[%s] token refresh failed: %v", slot.Name, err)
		} else {
			auth, err = loadAuthFile(slot.StateDir)
			if err != nil {
				return nil, err
			}
		}
	}
	if strings.TrimSpace(auth.Tokens.AccessToken) == "" {
		return nil, fmt.Errorf("slot %s has no access token", slot.Name)
	}

	accountID := strings.TrimSpace(slot.AccountID)
	if accountID == "" {
		accountID = strings.TrimSpace(auth.Tokens.AccountID)
	}
	if accountID == "" {
		accountID = extractAccountIDFromJWT(auth.Tokens.AccessToken)
	}
	if accountID == "" {
		return nil, fmt.Errorf("slot %s has no ChatGPT account id", slot.Name)
	}

	return &AuthContext{
		SlotID:      slot.ID,
		SlotName:    slot.Name,
		BearerToken: strings.TrimSpace(auth.Tokens.AccessToken),
		AccountID:   accountID,
	}, nil
}

func (m *Manager) markSlotState(slotID int64, status string, cooldownUntil time.Time, lastError string, markUsed bool) error {
	slot, err := m.storage.GetCodexSlot(slotID)
	if err != nil {
		return err
	}
	if slot == nil {
		return fmt.Errorf("slot not found: %d", slotID)
	}
	slot.Status = status
	slot.CooldownUntil = cooldownUntil
	slot.LastError = lastError
	slot.LastCheckedAt = time.Now()
	if markUsed {
		slot.LastUsedAt = time.Now()
	}
	return m.storage.UpdateCodexSlot(slot)
}

func loadAuthFile(stateDir string) (*authFile, error) {
	data, err := os.ReadFile(filepath.Join(stateDir, "auth.json"))
	if err != nil {
		return nil, err
	}
	var auth authFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, err
	}
	return &auth, nil
}

func writeAuthFile(stateDir string, auth *authFile) error {
	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, "auth.json"), data, 0o600)
}

func needsRefresh(auth *authFile) bool {
	exp, ok := extractExpiry(auth.Tokens.AccessToken)
	if !ok {
		return false
	}
	return time.Until(exp) < 10*time.Minute && strings.TrimSpace(auth.Tokens.RefreshToken) != ""
}

func refreshAuthFile(stateDir string, auth *authFile) error {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", strings.TrimSpace(auth.Tokens.RefreshToken))
	form.Set("client_id", defaultRefreshClientID)

	refreshURL := os.Getenv("CODEX_REFRESH_TOKEN_URL_OVERRIDE")
	if strings.TrimSpace(refreshURL) == "" {
		refreshURL = defaultRefreshURL
	}

	req, err := http.NewRequest(http.MethodPost, refreshURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("refresh token failed: http %d", resp.StatusCode)
	}

	var refresh refreshResponse
	if err := json.Unmarshal(body, &refresh); err != nil {
		return err
	}
	if strings.TrimSpace(refresh.AccessToken) == "" {
		return fmt.Errorf("refresh response missing access_token")
	}

	auth.Tokens.AccessToken = refresh.AccessToken
	if strings.TrimSpace(refresh.RefreshToken) != "" {
		auth.Tokens.RefreshToken = refresh.RefreshToken
	}
	if strings.TrimSpace(refresh.IDToken) != "" {
		auth.Tokens.IDToken = refresh.IDToken
	}
	auth.LastRefresh = time.Now().UTC().Format(time.RFC3339Nano)
	if auth.Tokens.AccountID == "" {
		auth.Tokens.AccountID = extractAccountIDFromJWT(refresh.AccessToken)
	}
	return writeAuthFile(stateDir, auth)
}

func exchangeAuthorizationCode(code, redirectURL, codeVerifier string) (*authorizationCodeResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", strings.TrimSpace(code))
	form.Set("client_id", defaultRefreshClientID)
	form.Set("redirect_uri", redirectURL)
	form.Set("code_verifier", codeVerifier)

	req, err := http.NewRequest(http.MethodPost, defaultRefreshURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("authorization code exchange failed: http %d: %s", resp.StatusCode, summarizeOAuthErrorBody(body))
	}

	var tokenResp authorizationCodeResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, err
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return nil, fmt.Errorf("authorization code exchange missing access_token")
	}
	return &tokenResp, nil
}

func summarizeOAuthErrorBody(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "empty response body"
	}

	var payload struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		Message          string `json:"message"`
		Detail           string `json:"detail"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		parts := make([]string, 0, 2)
		if strings.TrimSpace(payload.Error) != "" {
			parts = append(parts, strings.TrimSpace(payload.Error))
		}
		if strings.TrimSpace(payload.ErrorDescription) != "" {
			parts = append(parts, strings.TrimSpace(payload.ErrorDescription))
		} else if strings.TrimSpace(payload.Message) != "" {
			parts = append(parts, strings.TrimSpace(payload.Message))
		} else if strings.TrimSpace(payload.Detail) != "" {
			parts = append(parts, strings.TrimSpace(payload.Detail))
		}
		if len(parts) > 0 {
			return strings.Join(parts, ": ")
		}
	}

	if len(trimmed) > 240 {
		return trimmed[:240] + "..."
	}
	return trimmed
}

func (m *Manager) persistSlotAuth(slot *storage.CodexSlot, tokenResp *authorizationCodeResponse) error {
	if slot == nil {
		return fmt.Errorf("slot is nil")
	}

	accountID := extractAccountIDFromJWT(tokenResp.AccessToken)
	if accountID == "" {
		accountID = extractAccountIDFromJWT(tokenResp.IDToken)
	}
	if accountID == "" {
		return fmt.Errorf("authorization succeeded but account_id was not found")
	}

	auth := &authFile{
		AuthMode:    "chatgpt",
		APIKey:      nil,
		LastRefresh: time.Now().UTC().Format(time.RFC3339Nano),
	}
	auth.Tokens.AccessToken = strings.TrimSpace(tokenResp.AccessToken)
	auth.Tokens.RefreshToken = strings.TrimSpace(tokenResp.RefreshToken)
	auth.Tokens.IDToken = strings.TrimSpace(tokenResp.IDToken)
	auth.Tokens.AccountID = accountID

	if err := writeAuthFile(slot.StateDir, auth); err != nil {
		return err
	}

	slot.AccountID = accountID
	slot.LastError = ""
	slot.Status = SlotStatusReady
	slot.CooldownUntil = time.Time{}
	slot.LastCheckedAt = time.Now()
	if err := m.storage.UpdateCodexSlot(slot); err != nil {
		return err
	}
	return nil
}

func extractExpiry(token string) (time.Time, bool) {
	payload, ok := parseJWTClaims(token)
	if !ok {
		return time.Time{}, false
	}
	exp, ok := payload["exp"].(float64)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(int64(exp), 0), true
}

func extractAccountIDFromJWT(token string) string {
	payload, ok := parseJWTClaims(token)
	if !ok {
		return ""
	}
	if direct, ok := payload["chatgpt_account_id"].(string); ok {
		return strings.TrimSpace(direct)
	}
	if authObj, ok := payload["https://api.openai.com/auth"].(map[string]interface{}); ok {
		if accountID, ok := authObj["chatgpt_account_id"].(string); ok {
			return strings.TrimSpace(accountID)
		}
		if accountID, ok := authObj["account_id"].(string); ok {
			return strings.TrimSpace(accountID)
		}
	}
	return ""
}

func parseJWTClaims(token string) (map[string]interface{}, bool) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, false
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func sanitizeName(name string) string {
	trimmed := strings.TrimSpace(strings.ToLower(name))
	replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-", "@", "-", ".", "-")
	trimmed = replacer.Replace(trimmed)
	trimmed = strings.Trim(trimmed, "-")
	if trimmed == "" {
		return fmt.Sprintf("slot-%d", time.Now().Unix())
	}
	return trimmed
}

func dedupeSlotIDs(slotIDs []int64) []int64 {
	seen := make(map[int64]struct{}, len(slotIDs))
	result := make([]int64, 0, len(slotIDs))
	for _, slotID := range slotIDs {
		if slotID <= 0 {
			continue
		}
		if _, ok := seen[slotID]; ok {
			continue
		}
		seen[slotID] = struct{}{}
		result = append(result, slotID)
	}
	return result
}

func buildAuthorizeURL(redirectURL, state, codeChallenge string) string {
	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", defaultRefreshClientID)
	values.Set("redirect_uri", redirectURL)
	values.Set("scope", defaultAuthScope)
	values.Set("state", state)
	values.Set("code_challenge", codeChallenge)
	values.Set("code_challenge_method", "S256")
	values.Set("prompt", "login")
	values.Set("codex_cli_simplified_flow", "true")
	values.Set("id_token_add_organizations", "true")
	return defaultAuthorizeURL + "?" + values.Encode()
}

func codeChallengeS256(codeVerifier string) string {
	sum := sha256.Sum256([]byte(codeVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func generateCodeVerifier() (string, error) {
	return randomOAuthString(64)
}

func randomOAuthString(byteLen int) (string, error) {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func renderAuthCallbackPage(success bool, message string) string {
	title := "Codex Slot Login"
	color := "#1f8f53"
	status := "Authorization complete. You can close this tab."
	if !success {
		color = "#cc3d3d"
		status = "Authorization failed. Return to ccNexus and try again."
	}
	return fmt.Sprintf(`<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>%s</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; background: #f6f8fa; color: #222; padding: 40px 24px; }
    .card { max-width: 560px; margin: 0 auto; background: #fff; border: 1px solid #e5e7eb; border-radius: 12px; padding: 24px; }
    h1 { margin: 0 0 12px; font-size: 22px; color: %s; }
    p { line-height: 1.6; margin: 8px 0; word-break: break-word; }
  </style>
</head>
<body>
  <div class="card">
    <h1>%s</h1>
    <p>%s</p>
    <p>%s</p>
  </div>
</body>
</html>`, title, color, title, status, message)
}

func writeAuthCallbackResponse(w http.ResponseWriter, statusCode int, success bool, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = io.WriteString(w, renderAuthCallbackPage(success, message))
}
