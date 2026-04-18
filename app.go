package main

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"wcj-go-common/core"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	_ "modernc.org/sqlite"
)

// App struct
type App struct {
	ctx         context.Context
	SettingsDb  *sql.DB // 配置存储的 sqlite 数据库
	ProjectName string
	Assets      embed.FS
	mu          sync.Mutex
}

// NewApp creates a new App application instance
func NewApp(assets embed.FS) *App {
	return &App{
		Assets: assets,
	}
}

// startup is called when the application starts
func (a *App) Startup(ctx context.Context) {
	log.Printf("Startup 被调用")
	a.ctx = ctx
}

// shutdown is called when the application is about to quit
func (a *App) Shutdown(ctx context.Context) {
	// 关闭数据库
	if a.SettingsDb != nil {
		a.SettingsDb.Close()
	}
}

// InitSettingsDb 初始化配置数据库（sqlite）
func (a *App) InitSettingsDb() error {
	dbPath := core.GetTempDir() + "/data/sync_list.db"
	// 确保目录存在
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("创建数据库目录失败: %v", err)
	}

	// 使用 database/sql + sqlite3 驱动
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %v", err)
	}

	// 创建配置表
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS settings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key TEXT NOT NULL UNIQUE,
			value TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		db.Close()
		return fmt.Errorf("创建表失败: %v", err)
	}

	// 创建索引
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_settings_key ON settings(key)`)
	if err != nil {
		db.Close()
		return fmt.Errorf("创建索引失败: %v", err)
	}

	// 创建同步日志表
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS sync_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_name TEXT NOT NULL,
			repo_path TEXT NOT NULL,
			time TEXT,
			success INTEGER NOT NULL DEFAULT 0,
			message TEXT,
			commit_log TEXT,
			pull_log TEXT,
			push_log TEXT
		)
	`)
	if err != nil {
		db.Close()
		return fmt.Errorf("创建同步日志表失败: %v", err)
	}

	// 创建索引
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sync_logs_repo_path ON sync_logs(repo_path)`)
	if err != nil {
		db.Close()
		return fmt.Errorf("创建同步日志索引失败: %v", err)
	}

	// 创建Git仓库表
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS git_repos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			branch TEXT,
			remote TEXT,
			remote_url TEXT,
			last_sync_time TEXT,
			status TEXT,
			enabled INTEGER NOT NULL DEFAULT 1,
			auto_sync INTEGER NOT NULL DEFAULT 0,
			commit_only INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		db.Close()
		return fmt.Errorf("创建Git仓库表失败: %v", err)
	}

	// 创建索引
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_git_repos_path ON git_repos(path)`)
	if err != nil {
		db.Close()
		return fmt.Errorf("创建Git仓库索引失败: %v", err)
	}

	// 添加enabled列（如果不存在）
	_, err = db.Exec(`ALTER TABLE git_repos ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		db.Close()
		return fmt.Errorf("添加enabled列失败: %v", err)
	}

	// 添加commit_only列（如果不存在）
	_, err = db.Exec(`ALTER TABLE git_repos ADD COLUMN commit_only INTEGER NOT NULL DEFAULT 0`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		db.Close()
		return fmt.Errorf("添加commit_only列失败: %v", err)
	}

	// 创建窗口状态表
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS window_state (
			id INTEGER PRIMARY KEY,
			width INTEGER,
			height INTEGER,
			x INTEGER,
			y INTEGER,
			maximized INTEGER DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		db.Close()
		return fmt.Errorf("创建窗口状态表失败: %v", err)
	}

	a.SettingsDb = db
	return nil
}

// WindowState 窗口状态
type WindowState struct {
	Width     int `json:"width"`
	Height    int `json:"height"`
	X         int `json:"x"`
	Y         int `json:"y"`
	Maximized int `json:"maximized"`
}

// GetWindowState 获取窗口状态
func (a *App) GetWindowState() WindowState {
	if a.SettingsDb == nil {
		return WindowState{}
	}

	row := a.SettingsDb.QueryRow("SELECT width, height, x, y, maximized FROM window_state WHERE id = 1")
	var ws WindowState
	err := row.Scan(&ws.Width, &ws.Height, &ws.X, &ws.Y, &ws.Maximized)
	if err != nil {
		return WindowState{}
	}
	return ws
}

// SaveWindowState 保存窗口状态
func (a *App) SaveWindowState(ws WindowState) error {
	if a.SettingsDb == nil {
		return fmt.Errorf("数据库未初始化")
	}

	_, err := a.SettingsDb.Exec(`
		INSERT OR REPLACE INTO window_state (id, width, height, x, y, maximized, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, ws.Width, ws.Height, ws.X, ws.Y, ws.Maximized)
	return err
}

// SaveCurrentWindowState 捕获并保存当前窗口状态
func (a *App) SaveCurrentWindowState() error {
	if a.ctx == nil {
		return fmt.Errorf("context未初始化")
	}
	width, height := runtime.WindowGetSize(a.ctx)
	x, y := runtime.WindowGetPosition(a.ctx)
	maximized := 0
	if runtime.WindowIsMaximised(a.ctx) {
		maximized = 1
	}
	return a.SaveWindowState(WindowState{
		Width:     width,
		Height:    height,
		X:         x,
		Y:         y,
		Maximized: maximized,
	})
}

// SelectDirectory 打开目录选择对话框
func (a *App) SelectDirectory() (string, error) {
	selection, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择文件夹",
	})
	if err != nil {
		return "", err
	}
	return selection, nil
}

// ==================== Git同步功能 ====================

// GitRepo Git仓库信息
type GitRepo struct {
	Path         string `json:"path"`         // 仓库路径
	Name         string `json:"name"`         // 仓库名称
	Branch       string `json:"branch"`       // 当前分支
	Remote       string `json:"remote"`       // 远程仓库名
	RemoteUrl    string `json:"remoteUrl"`    // 远程仓库URL
	LastSyncTime string `json:"lastSyncTime"` // 上次同步时间
	Status       string `json:"status"`       // 状态
	Enabled      bool   `json:"enabled"`      // 是否启用
	AutoSync     bool   `json:"autoSync"`     // 是否自动同步
	CommitOnly   bool   `json:"commitOnly"`   // 仅提交，不推送
}

// GitSyncReq Git同步请求
type GitSyncReq struct {
	Repos []GitRepo `json:"repos"` // 要同步的仓库列表
}

// GitSyncResult 单个仓库同步结果
type GitSyncResult struct {
	Path      string `json:"path"`      // 仓库路径
	Name      string `json:"name"`      // 仓库名称
	Success   bool   `json:"success"`   // 是否成功
	Message   string `json:"message"`   // 结果信息
	PullLog   string `json:"pullLog"`   // pull输出
	PushLog   string `json:"pushLog"`   // push输出
	CommitLog string `json:"commitLog"` // commit输出
	Committed bool   `json:"committed"` // 是否提交了更改
	Pushed    bool   `json:"pushed"`    // 是否推送了更改
}

// GitSyncRes Git同步结果
type GitSyncRes struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Results []GitSyncResult `json:"results"`
}

// GitSync 同步Git仓库（调用后台服务）
func (a *App) GitSync(req GitSyncReq) GitSyncRes {
	resp, err := http.Post("http://localhost:9090/sync", "application/json", nil)
	if err != nil {
		return GitSyncRes{
			Success: false,
			Message: "调用同步服务失败: " + err.Error(),
		}
	}
	defer resp.Body.Close()

	return GitSyncRes{
		Success: true,
		Message: fmt.Sprintf("已调用同步服务"),
		Results: []GitSyncResult{},
	}
}

// GetGitRepoInfo 获取Git仓库信息
type GetGitRepoInfoReq struct {
	Path string `json:"path"` // 仓库路径
}

// GetGitRepoInfoRes 获取仓库信息结果
type GetGitRepoInfoRes struct {
	Success   bool     `json:"success"`
	Message   string   `json:"message"`
	Repo      *GitRepo `json:"repo"`
	IsGitRepo bool     `json:"isGitRepo"` // 是否是git仓库
}

// GetGitRepoInfo 获取Git仓库信息
func (a *App) GetGitRepoInfo(req GetGitRepoInfoReq) GetGitRepoInfoRes {
	if req.Path == "" {
		return GetGitRepoInfoRes{
			Success: false,
			Message: "请输入仓库路径",
		}
	}

	// 检查目录是否存在
	if _, err := os.Stat(req.Path); os.IsNotExist(err) {
		return GetGitRepoInfoRes{
			Success: false,
			Message: "目录不存在",
		}
	}

	// 检查是否是git仓库
	isGitRepo := false
	if _, err := os.Stat(filepath.Join(req.Path, ".git")); err == nil {
		isGitRepo = true
	}

	if !isGitRepo {
		return GetGitRepoInfoRes{
			Success:   false,
			Message:   "不是Git仓库",
			IsGitRepo: false,
		}
	}

	// 获取当前分支
	branchOutput, _ := RunWithDirAndOutput(req.Path, "git", "rev-parse", "--abbrev-ref", "HEAD")
	branch := strings.TrimSpace(string(branchOutput))

	// 获取远程仓库信息
	remoteOutput, _ := RunWithDirAndOutput(req.Path, "git", "remote", "get-url", "origin")
	remoteUrl := strings.TrimSpace(string(remoteOutput))

	// 获取仓库名称
	repoName := filepath.Base(req.Path)

	repo := &GitRepo{
		Path:      req.Path,
		Name:      repoName,
		Branch:    branch,
		Remote:    "origin",
		RemoteUrl: remoteUrl,
		Status:    "就绪",
		Enabled:   true,
	}

	return GetGitRepoInfoRes{
		Success:   true,
		Message:   "获取成功",
		Repo:      repo,
		IsGitRepo: true,
	}
}

// GitRepoListReq 仓库列表请求
type GitRepoListReq struct {
	Repos []GitRepo `json:"repos"`
}

// GitRepoListRes 仓库列表结果
type GitRepoListRes struct {
	Success bool      `json:"success"`
	Message string    `json:"message"`
	Repos   []GitRepo `json:"repos"`
}

// SaveGitRepoList 保存仓库列表到SQLite
func (a *App) SaveGitRepoList(req GitRepoListReq) GitRepoListRes {
	if a.SettingsDb == nil {
		return GitRepoListRes{
			Success: false,
			Message: "数据库未初始化",
		}
	}

	// 先删除所有现有仓库
	_, err := a.SettingsDb.Exec("DELETE FROM git_repos")
	if err != nil {
		return GitRepoListRes{
			Success: false,
			Message: "清空仓库列表失败: " + err.Error(),
		}
	}

	// 批量插入新仓库
	tx, err := a.SettingsDb.Begin()
	if err != nil {
		return GitRepoListRes{
			Success: false,
			Message: "开始事务失败: " + err.Error(),
		}
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO git_repos (path, name, branch, remote, remote_url, last_sync_time, status, enabled, auto_sync, commit_only)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return GitRepoListRes{
			Success: false,
			Message: "预处理失败: " + err.Error(),
		}
	}
	defer stmt.Close()

	for _, repo := range req.Repos {
		autoSync := 0
		if repo.AutoSync {
			autoSync = 1
		}
		enabled := 0
		if repo.Enabled {
			enabled = 1
		}
		commitOnly := 0
		if repo.CommitOnly {
			commitOnly = 1
		}
		_, err = stmt.Exec(repo.Path, repo.Name, repo.Branch, repo.Remote, repo.RemoteUrl, repo.LastSyncTime, repo.Status, enabled, autoSync, commitOnly)
		if err != nil {
			return GitRepoListRes{
				Success: false,
				Message: "保存仓库失败: " + err.Error(),
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return GitRepoListRes{
			Success: false,
			Message: "提交事务失败: " + err.Error(),
		}
	}

	// 通知 sync 服务刷新缓存
	go notifySyncRefresh()

	return GitRepoListRes{
		Success: true,
		Message: fmt.Sprintf("保存了 %d 个仓库", len(req.Repos)),
		Repos:   req.Repos,
	}
}

// LoadGitRepoList 从SQLite加载仓库列表
func (a *App) LoadGitRepoList() GitRepoListRes {
	if a.SettingsDb == nil {
		return GitRepoListRes{
			Success: false,
			Message: "数据库未初始化",
			Repos:   []GitRepo{},
		}
	}

	rows, err := a.SettingsDb.Query(`
		SELECT path, name, branch, remote, remote_url, last_sync_time, status, enabled, auto_sync, commit_only
		FROM git_repos ORDER BY id
	`)
	if err != nil {
		return GitRepoListRes{
			Success: false,
			Message: "查询失败: " + err.Error(),
			Repos:   []GitRepo{},
		}
	}
	defer rows.Close()

	repos := []GitRepo{}
	for rows.Next() {
		var repo GitRepo
		var enabled, autoSync, commitOnly int
		err := rows.Scan(&repo.Path, &repo.Name, &repo.Branch, &repo.Remote, &repo.RemoteUrl, &repo.LastSyncTime, &repo.Status, &enabled, &autoSync, &commitOnly)
		if err != nil {
			continue
		}
		repo.Enabled = enabled == 1
		repo.AutoSync = autoSync == 1
		repo.CommitOnly = commitOnly == 1
		repos = append(repos, repo)
	}

	return GitRepoListRes{
		Success: true,
		Message: fmt.Sprintf("加载了 %d 个仓库", len(repos)),
		Repos:   repos,
	}
}

// GitSyncLog 同步日志
type GitSyncLog struct {
	ID        int    `json:"id"`        // 日志ID
	RepoName  string `json:"repoName"`  // 仓库名称
	RepoPath  string `json:"repoPath"`  // 仓库路径
	Time      string `json:"time"`      // 同步时间
	Success   bool   `json:"success"`   // 是否成功
	Message   string `json:"message"`   // 结果信息
	CommitLog string `json:"commitLog"` // commit输出
	PullLog   string `json:"pullLog"`   // pull输出
	PushLog   string `json:"pushLog"`   // push输出
}

// GetSyncLogsReq 获取同步日志请求
type GetSyncLogsReq struct {
	RepoPath string `json:"repoPath"` // 仓库路径(可选)
	Limit    int    `json:"limit"`    // 获取条数
}

// GetSyncLogsRes 获取同步日志结果
type GetSyncLogsRes struct {
	Success bool         `json:"success"`
	Message string       `json:"message"`
	Logs    []GitSyncLog `json:"logs"`
}

// GetSyncLogs 获取同步日志
func (a *App) GetSyncLogs(req GetSyncLogsReq) GetSyncLogsRes {
	limit := req.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	if a.SettingsDb == nil {
		return GetSyncLogsRes{
			Success: false,
			Message: "数据库未初始化",
			Logs:    []GitSyncLog{},
		}
	}

	// 构建查询
	query := "SELECT id, repo_name, repo_path, time, success, message, commit_log, pull_log, push_log FROM sync_logs"
	args := []interface{}{}

	if req.RepoPath != "" {
		query += " WHERE repo_path = ?"
		args = append(args, req.RepoPath)
	}

	query += " ORDER BY time DESC LIMIT ?"
	args = append(args, limit)

	rows, err := a.SettingsDb.Query(query, args...)
	if err != nil {
		return GetSyncLogsRes{
			Success: false,
			Message: "查询失败: " + err.Error(),
			Logs:    []GitSyncLog{},
		}
	}
	defer rows.Close()

	logs := []GitSyncLog{}
	for rows.Next() {
		var syncLog GitSyncLog
		var success int
		err := rows.Scan(&syncLog.ID, &syncLog.RepoName, &syncLog.RepoPath, &syncLog.Time, &success, &syncLog.Message, &syncLog.CommitLog, &syncLog.PullLog, &syncLog.PushLog)
		if err != nil {
			log.Printf("扫描同步日志失败: %v", err)
			continue
		}
		syncLog.Success = success == 1
		logs = append(logs, syncLog)
	}

	return GetSyncLogsRes{
		Success: true,
		Message: fmt.Sprintf("共 %d 条日志", len(logs)),
		Logs:    logs,
	}
}

// SendToFrontend 发送消息到前端
func (a *App) SendToFrontend(event string, data interface{}) {
	runtime.EventsEmit(a.ctx, event, data)
}

// CopyToClipboard 复制到剪贴板
func (a *App) CopyToClipboard(text string) error {
	return runtime.ClipboardSetText(a.ctx, text)
}

// ==================== 辅助函数 ====================

// GetConfig 获取配置
func (a *App) GetConfig(key string) (string, error) {
	if a.SettingsDb == nil {
		return "", fmt.Errorf("数据库未初始化")
	}
	var value string
	err := a.SettingsDb.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

// SetConfig 设置配置
func (a *App) SetConfig(key, value string) error {
	if a.SettingsDb == nil {
		return fmt.Errorf("数据库未初始化")
	}
	_, err := a.SettingsDb.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = CURRENT_TIMESTAMP
	`, key, value, value)
	return err
}

// DeleteConfig 删除配置
func (a *App) DeleteConfig(key string) error {
	if a.SettingsDb == nil {
		return fmt.Errorf("数据库未初始化")
	}
	_, err := a.SettingsDb.Exec("DELETE FROM settings WHERE key = ?", key)
	return err
}

// GetJsonConfig 获取JSON配置
func (a *App) GetJsonConfig(key string, result interface{}) error {
	value, err := a.GetConfig(key)
	if err != nil {
		return err
	}
	if value == "" {
		return nil
	}
	return json.Unmarshal([]byte(value), result)
}

// SetJsonConfig 设置JSON配置
func (a *App) SetJsonConfig(key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return a.SetConfig(key, string(data))
}

// OpenUrl 打开URL
func (a *App) OpenUrl(url string) error {
	return OpenUrl(url)
}

// ReadEnvFiles 读取环境变量文件
func (a *App) ReadEnvFiles(dir string) ([]string, error) {
	var envFiles []string
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if !file.IsDir() && (strings.HasSuffix(file.Name(), ".env") || strings.HasSuffix(file.Name(), ".properties")) {
			envFiles = append(envFiles, filepath.Join(dir, file.Name()))
		}
	}
	return envFiles, nil
}

// ReadFile 读取文件
func (a *App) ReadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteFile 写入文件
func (a *App) WriteFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

// FileExists 检查文件是否存在
func (a *App) FileExists(path string) bool {
	exists, _ := core.PathExists(path)
	return exists
}

// GetTempDir 获取临时目录
func (a *App) GetTempDir() string {
	return core.GetTempDir()
}

// PathExists 检查路径是否存在
func (a *App) PathExists(path string) bool {
	exists, _ := core.PathExists(path)
	return exists
}

// CreateDir 创建目录
func (a *App) CreateDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// ListDir 列出目录
func (a *App) ListDir(dir string) ([]string, error) {
	var files []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		files = append(files, entry.Name())
	}
	return files, nil
}

// GetSystemEnv 获取系统环境变量
func (a *App) GetSystemEnv(name string) string {
	return os.Getenv(name)
}

// SetSystemEnv 设置系统环境变量（仅当前进程）
func (a *App) SetSystemEnv(name, value string) {
	os.Setenv(name, value)
}

// ==================== 对话框相关 ====================

// MessageDialog 消息对话框
func (a *App) MessageDialog(title, message string) error {
	_, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Title:   title,
		Message: message,
	})
	return err
}

// ConfirmDialog 确认对话框
func (a *App) ConfirmDialog(title, message string) bool {
	result, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Title:   title,
		Message: message,
		Type:    runtime.QuestionDialog,
		Buttons: []string{"确定", "取消"},
	})
	if err != nil {
		return false
	}
	return result == "确定"
}

// ==================== 窗口控制 ====================

// MinimizeWindow 最小化窗口
func (a *App) MinimizeWindow() {
	runtime.WindowMinimise(a.ctx)
}

// MaximizeWindow 最大化窗口
func (a *App) MaximizeWindow() {
	runtime.WindowMaximise(a.ctx)
}

// UnmaximizeWindow 取消最大化
func (a *App) UnmaximizeWindow() {
	runtime.WindowUnmaximise(a.ctx)
}

// CloseWindow 关闭窗口
func (a *App) CloseWindow() {
	runtime.Quit(a.ctx)
}

// HideWindow 隐藏窗口
func (a *App) HideWindow() {
	runtime.WindowHide(a.ctx)
}

// ShowWindow 显示窗口
func (a *App) ShowWindow() {
	runtime.WindowShow(a.ctx)
}

// IsWindowMaximized 窗口是否最大化
func (a *App) IsWindowMaximized() bool {
	return runtime.WindowIsMaximised(a.ctx)
}

// notifySyncRefresh 通知 sync 服务刷新缓存
func notifySyncRefresh() {
	resp, err := http.Post("http://localhost:9090/refresh", "application/json", nil)
	if err != nil {
		log.Printf("通知 sync 刷新失败: %v", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("已通知 sync 刷新缓存")
}

// ResetResult 重置结果
type ResetResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Output  string `json:"output"`
}

// ResetReq 重置请求
type ResetReq struct {
	Path string `json:"path"`
}

// ResetProject 重置项目（删除.git并重新初始化）
func (a *App) ResetProject(req ResetReq) ResetResult {
	projectDir := req.Path
	log.Printf("开始重置项目, 目录: %s", projectDir)

	var output string

	// 使用 git 命令获取分支名
	branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchCmd.Dir = projectDir
	branchCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	branchOut, branchErr := branchCmd.CombinedOutput()
	var branch string
	if branchErr == nil {
		branch = strings.TrimSpace(string(branchOut))
	} else {
		branch = "master"
		log.Printf("获取分支名失败，使用默认值 master: %v", string(branchOut))
	}
	log.Printf("检测到分支: %s", branch)

	// 使用 git 命令获取远程地址
	remoteCmd := exec.Command("git", "remote", "get-url", "origin")
	remoteCmd.Dir = projectDir
	remoteCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	remoteOut, remoteErr := remoteCmd.CombinedOutput()
	var remoteURL string
	if remoteErr == nil {
		remoteURL = strings.TrimSpace(string(remoteOut))
	} else {
		log.Printf("获取远程地址失败: %v, 输出: %s", remoteErr, string(remoteOut))
	}
	log.Printf("检测到远程地址: %s", remoteURL)

	// 验证必需信息
	if branch == "" {
		return ResetResult{
			Success: false,
			Message: "未检测到分支名，请确保是有效的 Git 仓库",
			Output:  "",
		}
	}
	if remoteURL == "" {
		return ResetResult{
			Success: false,
			Message: "未检测到远程地址，请确保仓库已配置 remote origin",
			Output:  "",
		}
	}

	gitDir := filepath.Join(projectDir, ".git")

	// 1. rm -rf .git (使用 Go 实现)
	output += "rm -rf .git\n"
	if err := os.RemoveAll(gitDir); err != nil {
		output += err.Error() + "\n"
		log.Printf("删除 .git 失败: %v", err)
	} else {
		output += "成功\n"
	}

	// 2. git init -b <branch>
	initCmd := exec.Command("git", "init", "-b", branch)
	initCmd.Dir = projectDir
	initCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	initOut, initErr := initCmd.CombinedOutput()
	output += fmt.Sprintf("git init -b %s\n", branch)
	if initErr != nil {
		output += string(initOut) + "\n"
		log.Printf("git init 失败: %v", initErr)
	} else {
		output += "成功\n"
	}

	// 3. git add .
	addCmd := exec.Command("git", "add", ".")
	addCmd.Dir = projectDir
	addCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	addOut, addErr := addCmd.CombinedOutput()
	output += "git add .\n"
	if addErr != nil {
		output += string(addOut) + "\n"
		log.Printf("git add 失败: %v", addErr)
	} else {
		output += "成功\n"
	}

	// 4. git commit -m "基本功能实现V1.0"
	commitCmd := exec.Command("git", "commit", "-m", "基本功能实现V1.0")
	commitCmd.Dir = projectDir
	commitCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	commitOut, commitErr := commitCmd.CombinedOutput()
	output += "git commit -m \"基本功能实现V1.0\"\n"
	if commitErr != nil {
		output += string(commitOut) + "\n"
		log.Printf("git commit 失败: %v", commitErr)
	} else {
		output += "成功\n"
	}

	// 5. git remote add origin <url>
	remoteAddCmd := exec.Command("git", "remote", "add", "origin", remoteURL)
	remoteAddCmd.Dir = projectDir
	remoteAddCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_, remoteAddErr := remoteAddCmd.CombinedOutput()
	output += fmt.Sprintf("git remote add origin %s\n", remoteURL)
	if remoteAddErr != nil {
		// 可能是 remote 已存在，尝试 set-url
		setUrlCmd := exec.Command("git", "remote", "set-url", "origin", remoteURL)
		setUrlCmd.Dir = projectDir
		setUrlCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		setUrlOut, setUrlErr := setUrlCmd.CombinedOutput()
		if setUrlErr != nil {
			output += string(setUrlOut) + "\n"
		} else {
			output += "成功\n"
		}
	} else {
		output += "成功\n"
	}

	// 6. git push -f -u origin <branch>
	pushCmd := exec.Command("git", "push", "-f", "-u", "origin", branch)
	pushCmd.Dir = projectDir
	pushCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	pushOut, pushErr := pushCmd.CombinedOutput()
	output += fmt.Sprintf("git push -f -u origin %s\n", branch)
	if pushErr != nil {
		output += string(pushOut) + "\n"
		log.Printf("git push 失败: %v", pushErr)
	} else {
		output += "成功\n"
	}

	log.Printf("重置完成, 输出:\n%s", output)
	return ResetResult{
		Success: true,
		Message: "重置完成",
		Output:  output,
	}
}

// PackageResult 打包结果
type PackageResult struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	Output      string `json:"output"`
	OutputDir   string `json:"outputDir"`
}

// PackageReq 打包请求
type PackageReq struct {
	Path string `json:"path"`
}

// PackageProject 执行 wails build 打包
func (a *App) PackageProject(req PackageReq) PackageResult {
	projectDir := req.Path
	log.Printf("开始打包项目, 目录: %s", projectDir)

	// 检查 wails.json 是否存在
	wailsConfig := filepath.Join(projectDir, "wails.json")
	if _, err := os.Stat(wailsConfig); os.IsNotExist(err) {
		log.Printf("wails.json 不存在: %s", wailsConfig)
		return PackageResult{
			Success:   false,
			Message:   "不是 Wails 项目目录",
			Output:    "",
			OutputDir: "",
		}
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("wails", "build")
	cmd.Dir = projectDir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	log.Printf("执行命令: wails build, 工作目录: %s", projectDir)
	err := cmd.Run()

	output := stdout.String() + stderr.String()
	log.Printf("打包输出:\n%s", output)

	if err != nil {
		log.Printf("打包失败: %v", err)
		return PackageResult{
			Success:   false,
			Message:   "打包失败: " + err.Error(),
			Output:    output,
			OutputDir: "",
		}
	}

	// 打包产物在 projectDir/build/bin/ 目录下
	outputDir := filepath.Join(projectDir, "build", "bin")

	// 查找生成的 exe 文件
	var exeFile string
	entries, err := os.ReadDir(outputDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".exe") {
				exeFile = entry.Name()
				break
			}
		}
	}

	if exeFile == "" {
		log.Printf("打包成功但未找到 exe 文件, 输出目录: %s", outputDir)
		return PackageResult{
			Success:   true,
			Message:   "打包成功，但未找到 exe 文件",
			Output:    output,
			OutputDir: outputDir,
		}
	}

	// 复制到目标目录
	targetDir := "E:\\application\\我的工具箱"
	targetPath := filepath.Join(targetDir, exeFile)

	// 确保目标目录存在
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		log.Printf("创建目标目录失败: %v", err)
		return PackageResult{
			Success:   false,
			Message:   "打包成功但创建目标目录失败: " + err.Error(),
			Output:    output,
			OutputDir: outputDir,
		}
	}

	// 复制文件
	sourcePath := filepath.Join(outputDir, exeFile)
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		log.Printf("读取 exe 文件失败: %v", err)
		return PackageResult{
			Success:   false,
			Message:   "打包成功但读取 exe 失败: " + err.Error(),
			Output:    output,
			OutputDir: outputDir,
		}
	}

	if err := os.WriteFile(targetPath, data, 0755); err != nil {
		log.Printf("复制 exe 到目标目录失败: %v", err)
		return PackageResult{
			Success:   false,
			Message:   "打包成功但复制到目标目录失败: " + err.Error(),
			Output:    output,
			OutputDir: outputDir,
		}
	}

	log.Printf("打包成功, 已复制到: %s", targetPath)
	return PackageResult{
		Success:   true,
		Message:   "打包成功，已复制到: " + targetPath,
		Output:    output,
		OutputDir: targetPath,
	}
}
