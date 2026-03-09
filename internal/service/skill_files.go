package service

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SkillFilesService - Skills 文件管理服务
//
// 1) 架构设计
// - Service: 受限目录访问（baseDir = ~/.claude/skills）
// - API: ListSkillFiles / ReadSkillFile / WriteSkillFile
// - 安全：只允许 baseDir 内部的 regular file，拒绝 symlink 和 path traversal
//
// 2) 错误码（前端按 code 处理提示/交互）
// - NotExist: 文件不存在
// - PermissionDenied: 无权限
// - Conflict: modTime 不一致（外部修改）
// - TooLarge: 文件超过大小限制（2MB）
// - InvalidPath: 路径非法（绝对路径、..、symlink）
//
// 3) 安全约束
// - 路径校验：只接受相对路径，拒绝绝对路径和 .. 跳转
// - 拒绝 symlink：使用 Lstat 检测，避免越权访问
// - 只允许 regular file：拒绝目录、设备文件等
// - 大小限制：文件 >2MB 返回错误
// - 冲突检测：使用 expectedModTime 机制
// - 原子写：temp + rename 模式
type SkillFilesService struct {
	mu      sync.Mutex
	baseDir string
}

func NewSkillFilesService() (*SkillFilesService, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	baseDir := filepath.Join(home, ".claude", "skills")
	return &SkillFilesService{
		baseDir: baseDir,
	}, nil
}

type SkillErrorCode string

const (
	SkillOK              SkillErrorCode = "OK"
	SkillNotExist        SkillErrorCode = "NotExist"
	SkillPermissionDenied SkillErrorCode = "PermissionDenied"
	SkillConflict        SkillErrorCode = "Conflict"
	SkillTooLarge        SkillErrorCode = "TooLarge"
	SkillInvalidPath     SkillErrorCode = "InvalidPath"
	SkillUnknown         SkillErrorCode = "Unknown"
)

const (
	maxSkillFileSize = 2 * 1024 * 1024 // 2MB
)

type SkillFileInfo struct {
	RelPath string `json:"relPath"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"` // UnixMilli
	IsDir   bool   `json:"isDir"`
}

type SkillListResult struct {
	OK      bool           `json:"ok"`
	Code    SkillErrorCode `json:"code"`
	Message string         `json:"message,omitempty"`
	Files   []SkillFileInfo `json:"files,omitempty"`
}

type SkillReadResult struct {
	OK      bool           `json:"ok"`
	Code    SkillErrorCode `json:"code"`
	Message string         `json:"message,omitempty"`

	RelPath string `json:"relPath,omitempty"`
	Path    string `json:"path,omitempty"`

	Exists  bool   `json:"exists"`
	ModTime int64  `json:"modTime,omitempty"` // UnixMilli
	Size    int64  `json:"size,omitempty"`

	Content string `json:"content,omitempty"`
}

type SkillWriteResult struct {
	OK      bool           `json:"ok"`
	Code    SkillErrorCode `json:"code"`
	Message string         `json:"message,omitempty"`

	RelPath string `json:"relPath,omitempty"`
	Path    string `json:"path,omitempty"`

	Conflict bool  `json:"conflict,omitempty"`
	ModTime  int64 `json:"modTime,omitempty"` // UnixMilli (after write)
}

// ListSkillFiles 列出 skills 目录下的所有文件（递归）
func (s *SkillFilesService) ListSkillFiles() SkillListResult {
	// 确保 baseDir 存在
	if _, err := os.Stat(s.baseDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// 目录不存在，返回空列表（不是错误）
			return SkillListResult{
				OK:    true,
				Code:  SkillOK,
				Files: []SkillFileInfo{},
			}
		}
		c, m := classifySkillFSError(err)
		return SkillListResult{
			OK:      false,
			Code:    c,
			Message: m,
		}
	}

	var files []SkillFileInfo
	err := filepath.WalkDir(s.baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// 跳过无法访问的文件/目录
			return nil
		}

		// 跳过 baseDir 本身
		if path == s.baseDir {
			return nil
		}

		// 计算相对路径
		relPath, err := filepath.Rel(s.baseDir, path)
		if err != nil {
			return nil
		}

		// 获取文件信息
		info, err := d.Info()
		if err != nil {
			return nil
		}

		// 只收集 regular file（跳过目录、symlink 等）
		if !info.Mode().IsRegular() {
			return nil
		}

		files = append(files, SkillFileInfo{
			RelPath: relPath,
			Size:    info.Size(),
			ModTime: info.ModTime().UnixMilli(),
			IsDir:   false,
		})

		return nil
	})

	if err != nil {
		c, m := classifySkillFSError(err)
		return SkillListResult{
			OK:      false,
			Code:    c,
			Message: m,
		}
	}

	return SkillListResult{
		OK:    true,
		Code:  SkillOK,
		Files: files,
	}
}

// ListSkillDirs 列出 skills 目录下的第一层目录
func (s *SkillFilesService) ListSkillDirs() SkillListResult {
	// 确保 baseDir 存在
	if _, err := os.Stat(s.baseDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// 目录不存在，返回空列表（不是错误）
			return SkillListResult{
				OK:    true,
				Code:  SkillOK,
				Files: []SkillFileInfo{},
			}
		}
		c, m := classifySkillFSError(err)
		return SkillListResult{
			OK:      false,
			Code:    c,
			Message: m,
		}
	}

	// 使用 os.ReadDir 只读取第一层
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		c, m := classifySkillFSError(err)
		return SkillListResult{
			OK:      false,
			Code:    c,
			Message: m,
		}
	}

	var dirs []SkillFileInfo
	for _, entry := range entries {
		// 只收集目录，跳过文件
		if !entry.IsDir() {
			continue
		}

		// 跳过隐藏目录（以 . 开头）
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// 安全检查：拒绝 symlink 目录
		if info.Mode()&fs.ModeSymlink != 0 {
			continue
		}

		dirs = append(dirs, SkillFileInfo{
			RelPath: entry.Name(),
			Size:    0,
			ModTime: info.ModTime().UnixMilli(),
			IsDir:   true,
		})
	}

	return SkillListResult{
		OK:    true,
		Code:  SkillOK,
		Files: dirs,
	}
}

// ReadSkillDoc 读取指定目录下的 SKILL.md 文件
func (s *SkillFilesService) ReadSkillDoc(dirName string) SkillReadResult {
	// 构建路径：<dirName>/SKILL.md
	relPath := filepath.Join(dirName, "SKILL.md")
	return s.ReadSkillFile(relPath)
}

// ReadSkillFile 读取指定的 skill 文件
func (s *SkillFilesService) ReadSkillFile(relPath string) SkillReadResult {
	// 验证并解析路径
	absPath, code, msg := s.validateAndResolvePath(relPath)
	if code != SkillOK {
		return SkillReadResult{
			OK:      false,
			Code:    code,
			Message: msg,
			RelPath: relPath,
		}
	}

	// 使用 Lstat 检测 symlink（不跟随符号链接）
	info, err := os.Lstat(absPath)
	if err != nil {
		c, m := classifySkillFSError(err)
		return SkillReadResult{
			OK:      false,
			Code:    c,
			Message: m,
			RelPath: relPath,
			Path:    absPath,
			Exists:  false,
		}
	}

	// 拒绝 symlink
	if info.Mode()&fs.ModeSymlink != 0 {
		return SkillReadResult{
			OK:      false,
			Code:    SkillInvalidPath,
			Message: "symlink not allowed",
			RelPath: relPath,
			Path:    absPath,
			Exists:  true,
		}
	}

	// 只允许 regular file
	if !info.Mode().IsRegular() {
		return SkillReadResult{
			OK:      false,
			Code:    SkillInvalidPath,
			Message: "not a regular file",
			RelPath: relPath,
			Path:    absPath,
			Exists:  true,
		}
	}

	// 检查文件大小
	if info.Size() > maxSkillFileSize {
		return SkillReadResult{
			OK:      false,
			Code:    SkillTooLarge,
			Message: fmt.Sprintf("file too large (max %d bytes)", maxSkillFileSize),
			RelPath: relPath,
			Path:    absPath,
			Exists:  true,
			Size:    info.Size(),
		}
	}

	// 读取文件内容
	data, err := os.ReadFile(absPath)
	if err != nil {
		c, m := classifySkillFSError(err)
		return SkillReadResult{
			OK:      false,
			Code:    c,
			Message: m,
			RelPath: relPath,
			Path:    absPath,
			Exists:  true,
		}
	}

	return SkillReadResult{
		OK:      true,
		Code:    SkillOK,
		RelPath: relPath,
		Path:    absPath,
		Exists:  true,
		ModTime: info.ModTime().UnixMilli(),
		Size:    info.Size(),
		Content: string(data),
	}
}

// WriteSkillFile 写入 skill 文件（带冲突检测）
func (s *SkillFilesService) WriteSkillFile(relPath, content string, expectedModTime int64) SkillWriteResult {
	// 验证并解析路径
	absPath, code, msg := s.validateAndResolvePath(relPath)
	if code != SkillOK {
		return SkillWriteResult{
			OK:      false,
			Code:    code,
			Message: msg,
			RelPath: relPath,
		}
	}

	// 检查内容大小
	if len(content) > maxSkillFileSize {
		return SkillWriteResult{
			OK:      false,
			Code:    SkillTooLarge,
			Message: fmt.Sprintf("content too large (max %d bytes)", maxSkillFileSize),
			RelPath: relPath,
			Path:    absPath,
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 使用 Lstat 检查文件状态（不跟随 symlink）
	info, err := os.Lstat(absPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c, m := classifySkillFSError(err)
			return SkillWriteResult{
				OK:      false,
				Code:    c,
				Message: m,
				RelPath: relPath,
				Path:    absPath,
			}
		}
		// 文件不存在
		if expectedModTime != 0 {
			return SkillWriteResult{
				OK:       false,
				Code:     SkillConflict,
				Message:  "conflict: file missing (modTime mismatch)",
				RelPath:  relPath,
				Path:     absPath,
				Conflict: true,
			}
		}
	} else {
		// 文件存在，检查类型和冲突
		if info.Mode()&fs.ModeSymlink != 0 {
			return SkillWriteResult{
				OK:      false,
				Code:    SkillInvalidPath,
				Message: "symlink not allowed",
				RelPath: relPath,
				Path:    absPath,
			}
		}
		if !info.Mode().IsRegular() {
			return SkillWriteResult{
				OK:      false,
				Code:    SkillInvalidPath,
				Message: "not a regular file",
				RelPath: relPath,
				Path:    absPath,
			}
		}
		if expectedModTime != 0 && info.ModTime().UnixMilli() != expectedModTime {
			return SkillWriteResult{
				OK:       false,
				Code:     SkillConflict,
				Message:  "conflict: modTime mismatch",
				RelPath:  relPath,
				Path:     absPath,
				Conflict: true,
			}
		}
	}

	// 确保父目录存在
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		c, m := classifySkillFSError(err)
		return SkillWriteResult{
			OK:      false,
			Code:    c,
			Message: m,
			RelPath: relPath,
			Path:    absPath,
		}
	}

	// 原子写入
	if err := atomicWriteFile(absPath, []byte(content), 0644); err != nil {
		c, m := classifySkillFSError(err)
		return SkillWriteResult{
			OK:      false,
			Code:    c,
			Message: m,
			RelPath: relPath,
			Path:    absPath,
		}
	}

	// 获取写入后的 modTime
	after, err := os.Stat(absPath)
	if err != nil {
		c, m := classifySkillFSError(err)
		return SkillWriteResult{
			OK:      false,
			Code:    c,
			Message: m,
			RelPath: relPath,
			Path:    absPath,
		}
	}

	return SkillWriteResult{
		OK:      true,
		Code:    SkillOK,
		RelPath: relPath,
		Path:    absPath,
		ModTime: after.ModTime().UnixMilli(),
	}
}

// validateAndResolvePath 验证并解析相对路径为绝对路径
func (s *SkillFilesService) validateAndResolvePath(relPath string) (absPath string, code SkillErrorCode, message string) {
	// 拒绝空路径
	if strings.TrimSpace(relPath) == "" {
		return "", SkillInvalidPath, "empty path"
	}

	// 拒绝绝对路径
	if filepath.IsAbs(relPath) {
		return "", SkillInvalidPath, "absolute path not allowed"
	}

	// 清理路径（移除 . 和 ..）
	cleanPath := filepath.Clean(relPath)

	// 拒绝包含 .. 的路径（防止 path traversal）
	if strings.Contains(cleanPath, "..") {
		return "", SkillInvalidPath, "path traversal not allowed"
	}

	// 构建绝对路径
	absPath = filepath.Join(s.baseDir, cleanPath)

	// 二次校验：确保解析后的路径仍在 baseDir 内
	relCheck, err := filepath.Rel(s.baseDir, absPath)
	if err != nil || strings.HasPrefix(relCheck, "..") {
		return "", SkillInvalidPath, "path outside base directory"
	}

	return absPath, SkillOK, ""
}

// classifySkillFSError 分类文件系统错误
func classifySkillFSError(err error) (SkillErrorCode, string) {
	if err == nil {
		return SkillOK, ""
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return SkillNotExist, "file not found"
	case errors.Is(err, fs.ErrPermission):
		return SkillPermissionDenied, "permission denied"
	default:
		return SkillUnknown, err.Error()
	}
}

