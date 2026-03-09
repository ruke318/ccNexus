package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// AIConfigService - Desktop AI 配置管理后端实现（方案 A：最小改动）
//
// 1) 架构设计
// - Service: 白名单映射（product + fileKey -> 绝对路径 + 文件类型信息）
// - API: ReadAIConfig / WriteAIConfig / CreateAIConfig（供 Wails bindings 调用）
// - 安全：只允许 6 个固定文件，拒绝任意路径读写（防止 path traversal）
//
// 2) 错误码（前端按 code 处理提示/交互）
// - NotExist: 文件不存在（Read）/ 目标不存在但 expectedModTime != 0（Write）
// - PermissionDenied: 无权限（读/写/创建/创建目录）
// - InvalidJSON: 写入 JSON 文件时 json.Valid() 失败
// - Conflict: modTime 不一致（外部修改/删除/创建冲突）
//
// 3) 实现步骤（按优先级）
// - Step 1: 新建本文件（核心逻辑 + 白名单 + 原子写 + 错误分类）
// - Step 2: cmd/desktop/app.go 增加 3 个 Wails bindings（Read/Write/Create）
// - Step 3: 路径处理统一 os.UserHomeDir + filepath.Join
// - Step 4: 原子写：临时文件 + rename（同目录）
// - Step 5: JSON 二次校验（仅 .json）
// - Step 6: Permission/NotExist/Conflict 分类输出
//
// 4) 测试要点（建议补充单测/手测）
// - 路径解析：claude/codex/gemini + md/settings/config 白名单正确
// - 权限错误：只读目录/只读文件（PermissionDenied）
// - 文件不存在：Read 返回 NotExist；Create 成功；Write expectedModTime 冲突
// - JSON 错误：settings.json 写入 invalid JSON（InvalidJSON）
// - 冲突检测：读取后外部修改，再写入（Conflict）
type AIConfigService struct {
	mu sync.Mutex
}

func NewAIConfigService() *AIConfigService {
	return &AIConfigService{}
}

type AIConfigErrorCode string

const (
	AIConfigOK              AIConfigErrorCode = "OK"
	AIConfigNotExist        AIConfigErrorCode = "NotExist"
	AIConfigPermissionDenied AIConfigErrorCode = "PermissionDenied"
	AIConfigInvalidJSON     AIConfigErrorCode = "InvalidJSON"
	AIConfigConflict        AIConfigErrorCode = "Conflict"

	AIConfigInvalidArgument AIConfigErrorCode = "InvalidArgument"
	AIConfigAlreadyExists   AIConfigErrorCode = "AlreadyExists"
	AIConfigUnknown         AIConfigErrorCode = "Unknown"
)

type AIConfigReadResult struct {
	OK      bool              `json:"ok"`
	Code    AIConfigErrorCode `json:"code"`
	Message string            `json:"message,omitempty"`

	Product string `json:"product,omitempty"`
	FileKey string `json:"fileKey,omitempty"`
	Path    string `json:"path,omitempty"`

	Exists  bool  `json:"exists"`
	ModTime int64 `json:"modTime,omitempty"` // UnixMilli
	IsJSON  bool  `json:"isJSON,omitempty"`

	Content string `json:"content,omitempty"`
}

type AIConfigWriteResult struct {
	OK      bool              `json:"ok"`
	Code    AIConfigErrorCode `json:"code"`
	Message string            `json:"message,omitempty"`

	Product string `json:"product,omitempty"`
	FileKey string `json:"fileKey,omitempty"`
	Path    string `json:"path,omitempty"`

	Conflict bool  `json:"conflict,omitempty"`
	ModTime  int64 `json:"modTime,omitempty"` // UnixMilli (after write)
}

type AIConfigCreateResult struct {
	OK      bool              `json:"ok"`
	Code    AIConfigErrorCode `json:"code"`
	Message string            `json:"message,omitempty"`

	Product string `json:"product,omitempty"`
	FileKey string `json:"fileKey,omitempty"`
	Path    string `json:"path,omitempty"`

	ModTime int64 `json:"modTime,omitempty"` // UnixMilli
}

func (s *AIConfigService) ReadAIConfig(product, fileKey string) AIConfigReadResult {
	product = normalizeKey(product)
	fileKey = normalizeKey(fileKey)

	path, isJSON, code, msg := resolveAIConfigPath(product, fileKey)
	if code != AIConfigOK {
		return AIConfigReadResult{
			OK:      false,
			Code:    code,
			Message: msg,
			Product: product,
			FileKey: fileKey,
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		c, m := classifyFSError(err)
		return AIConfigReadResult{
			OK:      false,
			Code:    c,
			Message: m,
			Product: product,
			FileKey: fileKey,
			Path:    path,
			Exists:  false,
			IsJSON:  isJSON,
		}
	}
	if !info.Mode().IsRegular() {
		return AIConfigReadResult{
			OK:      false,
			Code:    AIConfigUnknown,
			Message: "target is not a regular file",
			Product: product,
			FileKey: fileKey,
			Path:    path,
			Exists:  true,
			IsJSON:  isJSON,
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		c, m := classifyFSError(err)
		return AIConfigReadResult{
			OK:      false,
			Code:    c,
			Message: m,
			Product: product,
			FileKey: fileKey,
			Path:    path,
			Exists:  true,
			IsJSON:  isJSON,
		}
	}

	return AIConfigReadResult{
		OK:      true,
		Code:    AIConfigOK,
		Product: product,
		FileKey: fileKey,
		Path:    path,
		Exists:  true,
		ModTime: info.ModTime().UnixMilli(),
		IsJSON:  isJSON,
		Content: string(data),
	}
}

func (s *AIConfigService) WriteAIConfig(product, fileKey, content string, expectedModTime int64) AIConfigWriteResult {
	product = normalizeKey(product)
	fileKey = normalizeKey(fileKey)

	path, isJSON, code, msg := resolveAIConfigPath(product, fileKey)
	if code != AIConfigOK {
		return AIConfigWriteResult{
			OK:      false,
			Code:    code,
			Message: msg,
			Product: product,
			FileKey: fileKey,
		}
	}

	if isJSON && !json.Valid([]byte(content)) {
		return AIConfigWriteResult{
			OK:      false,
			Code:    AIConfigInvalidJSON,
			Message: "invalid json",
			Product: product,
			FileKey: fileKey,
			Path:    path,
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	info, err := os.Stat(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c, m := classifyFSError(err)
			return AIConfigWriteResult{
				OK:      false,
				Code:    c,
				Message: m,
				Product: product,
				FileKey: fileKey,
				Path:    path,
			}
		}
		if expectedModTime != 0 {
			return AIConfigWriteResult{
				OK:       false,
				Code:     AIConfigConflict,
				Message:  "conflict: target missing (modTime mismatch)",
				Product:  product,
				FileKey:  fileKey,
				Path:     path,
				Conflict: true,
			}
		}
	} else {
		if expectedModTime != 0 && info.ModTime().UnixMilli() != expectedModTime {
			return AIConfigWriteResult{
				OK:       false,
				Code:     AIConfigConflict,
				Message:  "conflict: modTime mismatch",
				Product:  product,
				FileKey:  fileKey,
				Path:     path,
				Conflict: true,
			}
		}
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		c, m := classifyFSError(err)
		return AIConfigWriteResult{
			OK:      false,
			Code:    c,
			Message: m,
			Product: product,
			FileKey: fileKey,
			Path:    path,
		}
	}

	if err := atomicWriteFile(path, []byte(content), 0644); err != nil {
		c, m := classifyFSError(err)
		return AIConfigWriteResult{
			OK:      false,
			Code:    c,
			Message: m,
			Product: product,
			FileKey: fileKey,
			Path:    path,
		}
	}

	after, err := os.Stat(path)
	if err != nil {
		c, m := classifyFSError(err)
		return AIConfigWriteResult{
			OK:      false,
			Code:    c,
			Message: m,
			Product: product,
			FileKey: fileKey,
			Path:    path,
		}
	}

	return AIConfigWriteResult{
		OK:      true,
		Code:    AIConfigOK,
		Product: product,
		FileKey: fileKey,
		Path:    path,
		ModTime: after.ModTime().UnixMilli(),
	}
}

func (s *AIConfigService) CreateAIConfig(product, fileKey, initialContent string) AIConfigCreateResult {
	product = normalizeKey(product)
	fileKey = normalizeKey(fileKey)

	path, isJSON, code, msg := resolveAIConfigPath(product, fileKey)
	if code != AIConfigOK {
		return AIConfigCreateResult{
			OK:      false,
			Code:    code,
			Message: msg,
			Product: product,
			FileKey: fileKey,
		}
	}

	if initialContent == "" {
		initialContent = defaultInitialContent(path, isJSON)
	}
	if isJSON && !json.Valid([]byte(initialContent)) {
		return AIConfigCreateResult{
			OK:      false,
			Code:    AIConfigInvalidJSON,
			Message: "invalid json",
			Product: product,
			FileKey: fileKey,
			Path:    path,
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		c, m := classifyFSError(err)
		return AIConfigCreateResult{
			OK:      false,
			Code:    c,
			Message: m,
			Product: product,
			FileKey: fileKey,
			Path:    path,
		}
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return AIConfigCreateResult{
				OK:      false,
				Code:    AIConfigAlreadyExists,
				Message: "already exists",
				Product: product,
				FileKey: fileKey,
				Path:    path,
			}
		}
		c, m := classifyFSError(err)
		return AIConfigCreateResult{
			OK:      false,
			Code:    c,
			Message: m,
			Product: product,
			FileKey: fileKey,
			Path:    path,
		}
	}
	_, werr := f.WriteString(initialContent)
	cerr := f.Close()
	if werr != nil {
		_ = os.Remove(path)
		c, m := classifyFSError(werr)
		return AIConfigCreateResult{
			OK:      false,
			Code:    c,
			Message: m,
			Product: product,
			FileKey: fileKey,
			Path:    path,
		}
	}
	if cerr != nil {
		_ = os.Remove(path)
		c, m := classifyFSError(cerr)
		return AIConfigCreateResult{
			OK:      false,
			Code:    c,
			Message: m,
			Product: product,
			FileKey: fileKey,
			Path:    path,
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		c, m := classifyFSError(err)
		return AIConfigCreateResult{
			OK:      false,
			Code:    c,
			Message: m,
			Product: product,
			FileKey: fileKey,
			Path:    path,
		}
	}

	return AIConfigCreateResult{
		OK:      true,
		Code:    AIConfigOK,
		Product: product,
		FileKey: fileKey,
		Path:    path,
		ModTime: info.ModTime().UnixMilli(),
	}
}

func resolveAIConfigPath(product, fileKey string) (path string, isJSON bool, code AIConfigErrorCode, message string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, AIConfigUnknown, fmt.Sprintf("failed to get home directory: %v", err)
	}

	switch product {
	case "claude":
		switch fileKey {
		case "md":
			return filepath.Join(home, ".claude", "CLAUDE.md"), false, AIConfigOK, ""
		case "settings":
			return filepath.Join(home, ".claude", "settings.json"), true, AIConfigOK, ""
		default:
			return "", false, AIConfigInvalidArgument, "invalid fileKey for claude (allowed: md/settings)"
		}
	case "codex":
		switch fileKey {
		case "md":
			return filepath.Join(home, ".codex", "AGENTS.md"), false, AIConfigOK, ""
		case "config":
			return filepath.Join(home, ".codex", "config.toml"), false, AIConfigOK, ""
		default:
			return "", false, AIConfigInvalidArgument, "invalid fileKey for codex (allowed: md/config)"
		}
	case "gemini":
		switch fileKey {
		case "md":
			return filepath.Join(home, ".gemini", "GEMINI.md"), false, AIConfigOK, ""
		case "settings":
			return filepath.Join(home, ".gemini", "settings.json"), true, AIConfigOK, ""
		default:
			return "", false, AIConfigInvalidArgument, "invalid fileKey for gemini (allowed: md/settings)"
		}
	default:
		return "", false, AIConfigInvalidArgument, "invalid product (allowed: claude/codex/gemini)"
	}
}

func normalizeKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func defaultInitialContent(path string, isJSON bool) string {
	if isJSON || strings.HasSuffix(strings.ToLower(path), ".json") {
		return "{}\n"
	}
	return ""
}

func classifyFSError(err error) (AIConfigErrorCode, string) {
	if err == nil {
		return AIConfigOK, ""
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return AIConfigNotExist, "file not found"
	case errors.Is(err, fs.ErrPermission):
		return AIConfigPermissionDenied, "permission denied"
	default:
		return AIConfigUnknown, err.Error()
	}
}

func atomicWriteFile(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	tmp, err := os.CreateTemp(dir, base+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		// Windows: rename over existing file may fail; fallback to remove+rename.
		// NOTE: remove+rename is not strictly atomic on Windows, but avoids partial writes.
		if errors.Is(err, fs.ErrExist) || strings.Contains(strings.ToLower(err.Error()), "cannot create a file") {
			_ = os.Remove(path)
			if err2 := os.Rename(tmpName, path); err2 == nil {
				return nil
			}
		}
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
