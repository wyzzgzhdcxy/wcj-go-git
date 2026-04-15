package main

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"wcj-go-common/core"

	_ "github.com/mattn/go-sqlite3"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx           context.Context
	SettingsDb    *sql.DB // 配置存储的 sqlite 数据库
	ProjectName   string
	Assets        embed.FS
	LocalFilePort int
	mu            sync.Mutex
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
	// 程序启动时自动开始后台自动同步
	go a.startBackgroundSync()
}

// startBackgroundSync 启动后台自动同步（程序启动时自动运行）
func (a *App) startBackgroundSync() {
	log.Printf("startBackgroundSync 开始执行")
	// 等待数据库初始化完成
	time.Sleep(2 * time.Second)

	// 加载所有仓库
	reposRes := a.LoadGitRepoList()
	if !reposRes.Success || len(reposRes.Repos) == 0 {
		log.Println("没有找到需要自动同步的仓库")
		return
	}

	if len(reposRes.Repos) == 0 {
		log.Println("没有启用自动同步的仓库")
		return
	}

	// 启动自动同步（StartAutoSync 内部会筛选出启用自动同步的仓库）
	log.Printf("程序启动，自动开始后台同步，共 %d 个仓库", len(reposRes.Repos))
	a.StartAutoSync(StartAutoSyncReq{Repos: reposRes.Repos})
}

// shutdown is called when the application is about to quit
func (a *App) Shutdown(ctx context.Context) {
	// 停止自动同步
	a.StopAutoSync()
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
	db, err := sql.Open("sqlite3", dbPath)
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
			interval_seconds INTEGER NOT NULL DEFAULT 60,
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
	Path            string `json:"path"`            // 仓库路径
	Name            string `json:"name"`            // 仓库名称
	Branch          string `json:"branch"`          // 当前分支
	Remote          string `json:"remote"`          // 远程仓库名
	RemoteUrl       string `json:"remoteUrl"`       // 远程仓库URL
	LastSyncTime    string `json:"lastSyncTime"`    // 上次同步时间
	Status          string `json:"status"`          // 状态
	Enabled         bool   `json:"enabled"`         // 是否启用
	AutoSync        bool   `json:"autoSync"`        // 是否自动同步
	IntervalSeconds int    `json:"intervalSeconds"` // 同步间隔(秒)
	CommitOnly      bool   `json:"commitOnly"`      // 仅提交，不推送
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
}

// GitSyncRes Git同步结果
type GitSyncRes struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Results []GitSyncResult `json:"results"`
}

// GitSync 同步Git仓库
func (a *App) GitSync(req GitSyncReq) GitSyncRes {
	results := make([]GitSyncResult, 0)

	for _, repo := range req.Repos {
		result := GitSyncResult{
			Path: repo.Path,
			Name: repo.Name,
		}

		// 检查目录是否存在
		if _, err := os.Stat(repo.Path); os.IsNotExist(err) {
			result.Success = false
			result.Message = "目录不存在"
			results = append(results, result)
			continue
		}

		// 执行 git add .
		addOutput, addErr := RunWithDirAndOutput(repo.Path, "git", "add", "-A")
		result.CommitLog = string(addOutput)
		if addErr != nil {
			result.CommitLog += "\n错误: " + addErr.Error()
		}

		// 检查是否有需要提交的更改
		statusOutput, _ := RunWithDirAndOutput(repo.Path, "git", "status", "--porcelain")
		hasChanges := len(strings.TrimSpace(string(statusOutput))) > 0

		if hasChanges {
			// 执行 git commit
			commitMsg := fmt.Sprintf("Sync: %s", time.Now().Format("2006-01-02 15:04:05"))
			commitOutput, commitErr := RunWithDirAndOutput(repo.Path, "git", "commit", "-m", commitMsg)
			result.CommitLog += "\n" + string(commitOutput)
			if commitErr != nil {
				result.CommitLog += "\n错误: " + commitErr.Error()
			}
		} else {
			result.CommitLog += "\n没有需要提交的更改"
		}

		// 执行 git pull
		pullOutput, pullErr := RunWithDirAndOutput(repo.Path, "git", "pull", repo.Remote, repo.Branch)
		result.PullLog = string(pullOutput)
		if pullErr != nil {
			result.PullLog += "\n错误: " + pullErr.Error()
		}

		// 如果是仅提交模式，跳过 push
		if !repo.CommitOnly {
			// 执行 git push
			pushOutput, pushErr := RunWithDirAndOutput(repo.Path, "git", "push", repo.Remote, repo.Branch)
			result.PushLog = string(pushOutput)
			if pushErr != nil {
				result.PushLog += "\n错误: " + pushErr.Error()
			}
		} else {
			result.PushLog = "仅提交模式，跳过 push"
		}

		result.Success = true
		result.Message = "同步完成"
		results = append(results, result)

		// 保存同步日志到数据库
		a.saveSyncLog(GitSyncLog{
			RepoName:  repo.Name,
			RepoPath:  repo.Path,
			Time:      time.Now().Format("2006-01-02 15:04:05"),
			Success:   result.Success,
			Message:   result.Message,
			CommitLog: result.CommitLog,
			PullLog:   result.PullLog,
			PushLog:   result.PushLog,
		})

		// 更新仓库的上次同步时间
		repo.LastSyncTime = time.Now().Format("2006-01-02 15:04:05")
		log.Printf("准备更新仓库 %s 的同步时间为: %s", repo.Name, repo.LastSyncTime)
		a.updateRepoLastSyncTime(repo)

		log.Printf("Git同步完成: %s", repo.Path)
	}

	return GitSyncRes{
		Success: true,
		Message: fmt.Sprintf("同步了 %d 个仓库", len(results)),
		Results: results,
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
		INSERT INTO git_repos (path, name, branch, remote, remote_url, last_sync_time, status, enabled, auto_sync, interval_seconds, commit_only)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		_, err = stmt.Exec(repo.Path, repo.Name, repo.Branch, repo.Remote, repo.RemoteUrl, repo.LastSyncTime, repo.Status, enabled, autoSync, repo.IntervalSeconds, commitOnly)
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

	// 同步更新自动同步缓存
	updateAutoSyncReposCache(req.Repos)

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
		SELECT path, name, branch, remote, remote_url, last_sync_time, status, enabled, auto_sync, interval_seconds, commit_only
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
		err := rows.Scan(&repo.Path, &repo.Name, &repo.Branch, &repo.Remote, &repo.RemoteUrl, &repo.LastSyncTime, &repo.Status, &enabled, &autoSync, &repo.IntervalSeconds, &commitOnly)
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

// StartAutoSyncReq 启动自动同步请求
type StartAutoSyncReq struct {
	Repos []GitRepo `json:"repos"` // 要自动同步的仓库列表
}

// StartAutoSyncRes 启动自动同步结果
type StartAutoSyncRes struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// autoSyncTicker 自动同步定时器
var autoSyncTicker *time.Ticker
var autoSyncRunning bool
var autoSyncReposCache []GitRepo // 自动同步仓库缓存
var autoSyncReposMu sync.RWMutex // 保护缓存的读写锁

// StartAutoSync 启动自动同步
func (a *App) StartAutoSync(req StartAutoSyncReq) StartAutoSyncRes {
	log.Printf("StartAutoSync 被调用, autoSyncRunning=%v, 请求仓库数=%d", autoSyncRunning, len(req.Repos))
	if autoSyncRunning {
		return StartAutoSyncRes{
			Success: false,
			Message: "自动同步已在运行中",
		}
	}

	// 收集需要自动同步的仓库
	autoSyncRepos := make([]GitRepo, 0)
	for _, repo := range req.Repos {
		log.Printf("检查仓库 %s: AutoSync=%v, CommitOnly=%v, IntervalSeconds=%d", repo.Name, repo.AutoSync, repo.CommitOnly, repo.IntervalSeconds)
		if (repo.AutoSync || repo.CommitOnly) && repo.IntervalSeconds > 0 {
			autoSyncRepos = append(autoSyncRepos, repo)
		}
	}

	log.Printf("符合条件的仓库数: %d", len(autoSyncRepos))

	if len(autoSyncRepos) == 0 {
		return StartAutoSyncRes{
			Success: false,
			Message: "没有启用自动同步的仓库",
		}
	}

	autoSyncRunning = true

	log.Printf("StartAutoSync: 设置 autoSyncRunning=true")

	// 初始化缓存
	updateAutoSyncReposCache(autoSyncRepos)

	log.Printf("StartAutoSync: 缓存已更新，准备创建 ticker")

	// 每10秒检查一次
	autoSyncTicker = time.NewTicker(10 * time.Second)
	go func() {
		for range autoSyncTicker.C {
			log.Printf("定时任务触发")
			// 从缓存读取仓库列表
			repos := getAutoSyncReposCache()
			log.Printf("缓存中 %d 个仓库", len(repos))
			// 检查每个仓库是否需要同步
			for _, repo := range repos {
				a.doAutoSync(repo)
			}
		}
	}()

	log.Printf("启动自动同步，共 %d 个仓库", len(autoSyncRepos))

	return StartAutoSyncRes{
		Success: true,
		Message: fmt.Sprintf("已启动自动同步，共 %d 个仓库", len(autoSyncRepos)),
	}
}

// doAutoSync 执行自动同步
func (a *App) doAutoSync(repo GitRepo) {
	now := time.Now()
		lastSync, err := time.ParseInLocation("2006-01-02 15:04:05", repo.LastSyncTime, time.Local)
	
	// 如果解析失败（首次同步或格式错误），则立即同步
	if err != nil || lastSync.IsZero() {
		log.Printf("自动同步(首次或时间解析失败): %s", repo.Name)
	} else if now.Sub(lastSync).Seconds() < float64(repo.IntervalSeconds) {
		// 检查是否到达同步时间
		log.Printf("自动同步检查: %s, 距上次同步 %.0f 秒, 间隔 %d 秒, 跳过",
			repo.Name, now.Sub(lastSync).Seconds(), repo.IntervalSeconds)
		return
	}

	log.Printf("自动同步: %s", repo.Name)

	result := a.GitSync(GitSyncReq{Repos: []GitRepo{repo}})

	// 记录日志
	if len(result.Results) > 0 {
		syncResult := result.Results[0]
		a.saveSyncLog(GitSyncLog{
			RepoName:  repo.Name,
			RepoPath:  repo.Path,
			Time:      now.Format("2006-01-02 15:04:05"),
			Success:   syncResult.Success,
			Message:   syncResult.Message,
			CommitLog: syncResult.CommitLog,
			PullLog:   syncResult.PullLog,
			PushLog:   syncResult.PushLog,
		})
	}

	// 更新仓库的上次同步时间
	repo.LastSyncTime = now.Format("2006-01-02 15:04:05")
	a.updateRepoLastSyncTime(repo)
	// 更新缓存中的同步时间
	updateAutoSyncRepoInCache(repo.Path, repo.LastSyncTime)
}

// saveSyncLog 保存同步日志到SQLite
func (a *App) saveSyncLog(logEntry GitSyncLog) {
	if a.SettingsDb == nil {
		log.Println("数据库未初始化，无法保存同步日志")
		return
	}

	// 插入新日志
	_, err := a.SettingsDb.Exec(`
		INSERT INTO sync_logs (repo_name, repo_path, time, success, message, commit_log, pull_log, push_log)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, logEntry.RepoName, logEntry.RepoPath, logEntry.Time, logEntry.Success, logEntry.Message, logEntry.CommitLog, logEntry.PullLog, logEntry.PushLog)

	if err != nil {
		log.Printf("保存同步日志失败: %v", err)
		return
	}

	// 删除超过100条的旧日志
	_, err = a.SettingsDb.Exec(`
		DELETE FROM sync_logs WHERE id NOT IN (
			SELECT id FROM sync_logs ORDER BY time DESC LIMIT 100
		)
	`)
	if err != nil {
		log.Printf("清理旧同步日志失败: %v", err)
	}
}

// updateRepoLastSyncTime 更新仓库上次同步时间
func (a *App) updateRepoLastSyncTime(repo GitRepo) {
	if a.SettingsDb == nil {
		log.Println("数据库未初始化，无法更新仓库同步时间")
		return
	}

	result, err := a.SettingsDb.Exec("UPDATE git_repos SET last_sync_time = ?, updated_at = CURRENT_TIMESTAMP WHERE path = ?", repo.LastSyncTime, repo.Path)
	if err != nil {
		log.Printf("更新仓库同步时间失败: %v", err)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	log.Printf("更新仓库 %s 的同步时间: %s, 影响行数: %d", repo.Name, repo.LastSyncTime, rowsAffected)
}

// StopAutoSync 停止自动同步
func (a *App) StopAutoSync() {
	if autoSyncTicker != nil {
		autoSyncTicker.Stop()
		autoSyncTicker = nil
	}
	autoSyncRunning = false
	log.Println("自动同步已停止")
}

// GetAutoSyncStatus 获取自动同步状态
type GetAutoSyncStatusRes struct {
	Running bool      `json:"running"`
	Repos   []GitRepo `json:"repos"` // 正在自动同步的仓库
}

// GetAutoSyncStatus 获取自动同步状态
func (a *App) GetAutoSyncStatus() GetAutoSyncStatusRes {
	return GetAutoSyncStatusRes{
		Running: autoSyncRunning,
		Repos:   []GitRepo{},
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

// GzipBytes gzip压缩
func GzipBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer, _ := gzip.NewWriterLevel(&buf, flate.BestCompression)
	_, err := writer.Write(data)
	if err != nil {
		return nil, err
	}
	err = writer.Close()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GunzipBytes gzip解压
func GunzipBytes(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

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

// StartLocalFileServer 启动本地文件服务
func (a *App) StartLocalFileServer(port int) {
	a.LocalFilePort = port
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			filePath := r.URL.Path
			if filePath == "/" {
				filePath = "/index.html"
			}
			filePath = filepath.Join(core.GetTempDir(), filePath)

			data, err := os.ReadFile(filePath)
			if err != nil {
				w.WriteHeader(404)
				w.Write([]byte("File not found"))
				return
			}

			// 简单判断文件类型
			if strings.HasSuffix(filePath, ".html") {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
			} else if strings.HasSuffix(filePath, ".js") {
				w.Header().Set("Content-Type", "application/javascript")
			} else if strings.HasSuffix(filePath, ".css") {
				w.Header().Set("Content-Type", "text/css")
			}

			w.Write(data)
		})

		log.Printf("本地文件服务启动: http://localhost:%d", port)
		err := http.ListenAndServe(fmt.Sprintf(":%d", port), mux)
		if err != nil {
			log.Printf("本地文件服务错误: %v", err)
		}
	}()
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

// ==================== 自动同步缓存相关 ====================

// updateAutoSyncReposCache 更新自动同步仓库缓存
func updateAutoSyncReposCache(repos []GitRepo) {
	autoSyncReposMu.Lock()
	defer autoSyncReposMu.Unlock()

	autoSyncReposCache = make([]GitRepo, 0)
	for _, repo := range repos {
		if (repo.AutoSync || repo.CommitOnly) && repo.IntervalSeconds > 0 {
			autoSyncReposCache = append(autoSyncReposCache, repo)
		}
	}
	log.Printf("更新自动同步缓存，共 %d 个仓库", len(autoSyncReposCache))
}

// updateAutoSyncRepoInCache 更新缓存中指定仓库的同步时间
func updateAutoSyncRepoInCache(repoPath, lastSyncTime string) {
	autoSyncReposMu.Lock()
	defer autoSyncReposMu.Unlock()

	for i := range autoSyncReposCache {
		if autoSyncReposCache[i].Path == repoPath {
			autoSyncReposCache[i].LastSyncTime = lastSyncTime
			break
		}
	}
}

// getAutoSyncReposCache 获取自动同步仓库缓存的副本
func getAutoSyncReposCache() []GitRepo {
	autoSyncReposMu.RLock()
	defer autoSyncReposMu.RUnlock()

	repos := make([]GitRepo, len(autoSyncReposCache))
	copy(repos, autoSyncReposCache)
	return repos
}

// ==================== Scanner 相关 ====================

// ScanLine 扫描一行
func ScanLine(r *bufio.Reader) (string, error) {
	line, _, err := r.ReadLine()
	return string(line), err
}
