package main

import (
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"wcj-go-git/gitcmd"
	"wcj-go-git/types"

	"github.com/wyzzgzhdcxy/wcj-go-common/core"
	myUtil "github.com/wyzzgzhdcxy/wcj-go-common/utils"

	_ "modernc.org/sqlite"
)

// 本服务是 sync_list.db 的唯一访问入口：建表、迁移、读写全部在本进程完成，
// GUI 界面不直接访问数据库，所有数据操作都通过下方 HTTP 接口进行。

// initGitSsh 设置 git 使用的 SSH 私钥路径
func initGitSsh() {
	os.Setenv("GIT_SSH_COMMAND", "ssh -i "+getRsaPrivateKeyPath())
}

// autoSyncTicker 自动同步定时器
var autoSyncTicker *time.Ticker
var autoSyncRunning bool
var reposCache []types.GitRepo
var reposMu sync.RWMutex

// syncRunMu 串行化同步执行：手动同步与自动同步并发操作同一仓库会触发 git index.lock 冲突
var syncRunMu sync.Mutex

// lastAttempt 记录每个仓库最后一次同步尝试时间（无论成败），用于按仓库间隔调度
var lastAttempt = make(map[string]time.Time)
var lastAttemptMu sync.Mutex

func main() {
	// InitLog 会在 {用户缓存目录}/wtools 下创建日志文件，首次运行时目录可能不存在，必须先创建
	core.MkDirALl0755(core.GetTempDir())
	myUtil.InitLog(true)
	startSt := time.Now().Format("2006-01-02 15:04:05.000")
	initGitSsh()
	log.Printf("%s log init finish! %s", startSt, time.Now().Format("2006-01-02 15:04:05.000"))

	// 初始化数据库（含建表与迁移）
	db, err := initDb()
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer db.Close()

	// 初始加载参与自动同步的仓库缓存
	reposCache = loadAutoSyncRepos(db)

	// 启动自动同步
	startAutoSync(db)

	// 启动 HTTP 数据接口（阻塞主线程）
	startHttpServer(db)
}

// startHttpServer 数据接口：GUI 的仓库列表、同步日志、配置读写全部经由这些接口完成
func startHttpServer(db *sql.DB) {
	// 刷新自动同步缓存
	http.HandleFunc("/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		refreshCache(db)
		log.Printf("收到刷新请求，已更新缓存，共 %d 个仓库", len(reposCache))
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK: %d repos refreshed", len(reposCache))
	})

	// 手动同步接口：同步所有已启用仓库（范围与自动同步不同，因此直接查库而非用缓存），
	// 并把每个仓库的执行结果以 JSON 返回给 GUI 展示
	http.HandleFunc("/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		syncRunMu.Lock()
		defer syncRunMu.Unlock()

		repos := queryRepos(db, "enabled = 1")
		results := make([]types.SyncResult, 0, len(repos))
		for _, repo := range repos {
			results = append(results, doSync(db, repo))
		}
		writeJSON(w, types.SyncRes{Success: true, Results: results})
	})

	// 仓库列表（含最近一次同步结果），供界面展示
	http.HandleFunc("/repos/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		repos := loadAllRepos(db)
		writeJSON(w, types.RepoListRes{
			Success: true,
			Message: fmt.Sprintf("加载了 %d 个仓库", len(repos)),
			Repos:   repos,
		})
	})

	// 保存仓库列表（整体替换），保存后立即刷新自动同步缓存
	http.HandleFunc("/repos/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req types.RepoListReq
		myUtil.BindObject(r, &req)
		if err := saveRepos(db, req.Repos); err != nil {
			log.Printf("保存仓库列表失败: %v", err)
			writeJSON(w, types.RepoListRes{Success: false, Message: "保存仓库列表失败: " + err.Error()})
			return
		}
		refreshCache(db)
		writeJSON(w, types.RepoListRes{
			Success: true,
			Message: fmt.Sprintf("保存了 %d 个仓库", len(req.Repos)),
			Repos:   req.Repos,
		})
	})

	// 同步日志查询
	http.HandleFunc("/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req types.SyncLogsReq
		myUtil.BindObject(r, &req)
		writeJSON(w, queryLogs(db, req))
	})

	// 数据库内容指纹：GUI 轮询它检测数据变化，变化后再拉取列表/日志（GUI 不直接访问数据库）
	http.HandleFunc("/fingerprint", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, types.FingerprintRes{Fingerprint: dbFingerprint(db)})
	})

	// 配置读写（settings 表由服务独占）
	http.HandleFunc("/config/get", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req types.KeyValueReq
		myUtil.BindObject(r, &req)
		value, err := getConfig(db, req.Key)
		if err != nil {
			writeJSON(w, types.KeyValueRes{Success: false, Message: err.Error()})
			return
		}
		writeJSON(w, types.KeyValueRes{Success: true, Value: value})
	})

	http.HandleFunc("/config/set", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req types.KeyValueReq
		myUtil.BindObject(r, &req)
		if req.Key == "" {
			writeJSON(w, types.KeyValueRes{Success: false, Message: "key 不能为空"})
			return
		}
		if err := setConfig(db, req.Key, req.Value); err != nil {
			writeJSON(w, types.KeyValueRes{Success: false, Message: err.Error()})
			return
		}
		writeJSON(w, types.KeyValueRes{Success: true, Message: "OK"})
	})

	http.HandleFunc("/config/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req types.KeyValueReq
		myUtil.BindObject(r, &req)
		if err := deleteConfig(db, req.Key); err != nil {
			writeJSON(w, types.KeyValueRes{Success: false, Message: err.Error()})
			return
		}
		writeJSON(w, types.KeyValueRes{Success: true, Message: "OK"})
	})

	// 信号处理，优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 只监听本机回环地址，避免局域网内其他机器触发本机的 git 操作
	const port = "127.0.0.1:19090"
	ln, lnErr := net.Listen("tcp", port)
	if lnErr != nil {
		log.Fatalf("HTTP 服务器启动失败，端口 %s 不可用: %v", port, lnErr)
	}
	log.Printf("HTTP 数据接口已启动: http://%s", port)

	go func() {
		if err := http.Serve(ln, nil); err != nil {
			log.Printf("HTTP 服务器退出: %v", err)
		}
	}()

	<-quit
	_ = ln.Close()
	log.Println("收到退出信号，正在停止...")
	if autoSyncTicker != nil {
		autoSyncTicker.Stop()
	}
	_ = db.Close()
	os.Exit(0)
}

// writeJSON 以 JSON 响应
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(core.ToJsonString(v)))
}

// initDb 打开数据库并确保表结构与迁移到位（服务是数据库唯一属主）
func initDb() (*sql.DB, error) {
	dbPath := core.GetTempDir() + "/data/sync_list.db"
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %v", err)
	}

	// WAL 提升读写并发，busy_timeout 等待锁而不是立即报错
	if _, err = db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return nil, fmt.Errorf("设置 WAL 模式失败: %v", err)
	}
	if _, err = db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return nil, fmt.Errorf("设置 busy_timeout 失败: %v", err)
	}
	db.SetMaxOpenConns(1)

	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("数据库连接失败: %v", err)
	}

	// 建表
	ddls := []string{
		`CREATE TABLE IF NOT EXISTS settings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key TEXT NOT NULL UNIQUE,
			value TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_settings_key ON settings(key)`,
		`CREATE TABLE IF NOT EXISTS sync_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_name TEXT NOT NULL,
			repo_path TEXT NOT NULL,
			time TEXT,
			success INTEGER NOT NULL DEFAULT 0,
			message TEXT,
			commit_log TEXT,
			pull_log TEXT,
			push_log TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_logs_repo_path ON sync_logs(repo_path)`,
		`CREATE TABLE IF NOT EXISTS git_repos (
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
		)`,
		`CREATE INDEX IF NOT EXISTS idx_git_repos_path ON git_repos(path)`,
	}
	for _, ddl := range ddls {
		if _, err := db.Exec(ddl); err != nil {
			return nil, fmt.Errorf("初始化表结构失败: %v", err)
		}
	}

	// 旧库补列（新库建表时已包含，报 duplicate column name 属预期）
	migrations := []string{
		`ALTER TABLE git_repos ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE git_repos ADD COLUMN commit_only INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE git_repos ADD COLUMN sync_interval INTEGER NOT NULL DEFAULT 0`,
	}
	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return nil, fmt.Errorf("迁移列失败: %v", err)
		}
	}

	return db, nil
}

// queryRepos 按条件查询仓库列表
func queryRepos(db *sql.DB, where string) []types.GitRepo {
	rows, err := db.Query(`
		SELECT path, name, branch, remote, remote_url, last_sync_time, status, enabled, auto_sync, commit_only, COALESCE(sync_interval, 0)
		FROM git_repos WHERE ` + where + ` ORDER BY id
	`)
	if err != nil {
		log.Printf("查询仓库失败: %v", err)
		return nil
	}
	defer rows.Close()

	var repos []types.GitRepo
	for rows.Next() {
		var repo types.GitRepo
		var enabled, autoSync, commitOnly, interval int
		if err := rows.Scan(&repo.Path, &repo.Name, &repo.Branch, &repo.Remote, &repo.RemoteUrl, &repo.LastSyncTime, &repo.Status, &enabled, &autoSync, &commitOnly, &interval); err != nil {
			continue
		}
		repo.Enabled = enabled == 1
		repo.AutoSync = autoSync == 1
		repo.CommitOnly = commitOnly == 1
		repo.Interval = interval
		repos = append(repos, repo)
	}
	return repos
}

// loadAllRepos 全部仓库（含最近一次同步结果），供界面展示
func loadAllRepos(db *sql.DB) []types.GitRepo {
	rows, err := db.Query(`
		SELECT path, name, COALESCE(branch, ''), COALESCE(remote, ''), COALESCE(remote_url, ''),
			COALESCE(last_sync_time, ''), COALESCE(status, ''), enabled, auto_sync, commit_only, COALESCE(sync_interval, 0),
			COALESCE((SELECT success FROM sync_logs WHERE repo_path = git_repos.path ORDER BY id DESC LIMIT 1), -1) AS last_sync_success
		FROM git_repos ORDER BY id
	`)
	if err != nil {
		log.Printf("查询仓库失败: %v", err)
		return []types.GitRepo{}
	}
	defer rows.Close()

	repos := []types.GitRepo{}
	for rows.Next() {
		var repo types.GitRepo
		var enabled, autoSync, commitOnly, interval, lastSyncSuccess int
		if err := rows.Scan(&repo.Path, &repo.Name, &repo.Branch, &repo.Remote, &repo.RemoteUrl, &repo.LastSyncTime, &repo.Status, &enabled, &autoSync, &commitOnly, &interval, &lastSyncSuccess); err != nil {
			continue
		}
		repo.Enabled = enabled == 1
		repo.AutoSync = autoSync == 1
		repo.CommitOnly = commitOnly == 1
		repo.Interval = interval
		repo.LastSyncSuccess = lastSyncSuccess
		repos = append(repos, repo)
	}
	return repos
}

// loadEnabledRepos 所有已启用仓库（手动同步用）
func loadEnabledRepos(db *sql.DB) []types.GitRepo {
	return queryRepos(db, "enabled = 1")
}

// loadAutoSyncRepos 参与自动同步的仓库（开启自动同步或处于仅提交模式）
func loadAutoSyncRepos(db *sql.DB) []types.GitRepo {
	return queryRepos(db, "enabled = 1 AND (auto_sync = 1 OR commit_only = 1)")
}

// refreshCache 重新加载自动同步仓库缓存
func refreshCache(db *sql.DB) {
	repos := loadAutoSyncRepos(db)
	reposMu.Lock()
	reposCache = repos
	reposMu.Unlock()
}

// saveRepos 整体替换仓库列表，删除与插入在同一事务中，失败不会丢旧配置
func saveRepos(db *sql.DB, repos []types.GitRepo) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("开始事务失败: %v", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM git_repos"); err != nil {
		return fmt.Errorf("清空仓库列表失败: %v", err)
	}

	stmt, err := tx.Prepare(`
		INSERT INTO git_repos (path, name, branch, remote, remote_url, last_sync_time, status, enabled, auto_sync, commit_only, sync_interval)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("预处理失败: %v", err)
	}
	defer stmt.Close()

	for _, repo := range repos {
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
		if _, err := stmt.Exec(repo.Path, repo.Name, repo.Branch, repo.Remote, repo.RemoteUrl, repo.LastSyncTime, repo.Status, enabled, autoSync, commitOnly, repo.Interval); err != nil {
			return fmt.Errorf("保存仓库 %s 失败: %v", repo.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %v", err)
	}
	return nil
}

// queryLogs 查询同步日志
func queryLogs(db *sql.DB, req types.SyncLogsReq) types.SyncLogsRes {
	limit := req.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	query := "SELECT id, repo_name, repo_path, COALESCE(time, ''), success, COALESCE(message, ''), COALESCE(commit_log, ''), COALESCE(pull_log, ''), COALESCE(push_log, '') FROM sync_logs"
	args := []interface{}{}

	if req.RepoPath != "" {
		query += " WHERE repo_path = ?"
		args = append(args, req.RepoPath)
	}
	query += " ORDER BY time DESC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return types.SyncLogsRes{Success: false, Message: "查询失败: " + err.Error(), Logs: []types.SyncLog{}}
	}
	defer rows.Close()

	logs := []types.SyncLog{}
	for rows.Next() {
		var l types.SyncLog
		var success int
		if err := rows.Scan(&l.ID, &l.RepoName, &l.RepoPath, &l.Time, &success, &l.Message, &l.CommitLog, &l.PullLog, &l.PushLog); err != nil {
			log.Printf("扫描同步日志失败: %v", err)
			continue
		}
		l.Success = success == 1
		logs = append(logs, l)
	}
	return types.SyncLogsRes{Success: true, Message: fmt.Sprintf("共 %d 条日志", len(logs)), Logs: logs}
}

// dbFingerprint 数据库内容指纹：sync_logs 的最大 id 与行数、git_repos 的最大 updated_at 与行数
func dbFingerprint(db *sql.DB) string {
	var logsMaxID, logsCount int64
	var reposUpdatedAt string
	var reposCount int64
	err := db.QueryRow(`
		SELECT (SELECT COALESCE(MAX(id), 0) FROM sync_logs),
		       (SELECT COUNT(*) FROM sync_logs),
		       (SELECT COALESCE(MAX(updated_at), '') FROM git_repos),
		       (SELECT COUNT(*) FROM git_repos)
	`).Scan(&logsMaxID, &logsCount, &reposUpdatedAt, &reposCount)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d:%s:%d", logsMaxID, logsCount, reposUpdatedAt, reposCount)
}

// getConfig/setConfig/deleteConfig settings 表的读写
func getConfig(db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRow("SELECT COALESCE(value, '') FROM settings WHERE key = ?", key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

func setConfig(db *sql.DB, key, value string) error {
	_, err := db.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = CURRENT_TIMESTAMP
	`, key, value, value)
	return err
}

func deleteConfig(db *sql.DB, key string) error {
	_, err := db.Exec("DELETE FROM settings WHERE key = ?", key)
	return err
}

func startAutoSync(db *sql.DB) {
	if autoSyncRunning {
		log.Println("自动同步已在运行中")
		return
	}

	autoSyncRunning = true
	log.Println("启动自动同步")

	autoSyncTicker = time.NewTicker(1 * time.Minute)
	go func() {
		for now := range autoSyncTicker.C {
			log.Printf("轮询定时任务触发")
			reposMu.RLock()
			repos := reposCache
			reposMu.RUnlock()

			syncRunMu.Lock()
			for _, repo := range repos {
				// 按仓库独立间隔调度：间隔未到（或刚被手动同步过）的仓库本轮跳过
				if !dueForSync(repo, now) {
					continue
				}
				doSync(db, repo)
			}
			syncRunMu.Unlock()
		}
	}()
}

// dueForSync 判断仓库本轮是否到达同步时间：interval=0 表示每次轮询都同步，
// 否则距上次同步尝试（含手动同步）超过 interval 分钟才同步
func dueForSync(repo types.GitRepo, now time.Time) bool {
	if repo.Interval <= 0 {
		return true
	}
	lastAttemptMu.Lock()
	defer lastAttemptMu.Unlock()
	last, ok := lastAttempt[repo.Path]
	if !ok {
		return true // 进程启动后首次出现，立即同步一次
	}
	return now.Sub(last) >= time.Duration(repo.Interval)*time.Minute
}

func doSync(db *sql.DB, repo types.GitRepo) types.SyncResult {
	now := time.Now()
	// 记录本次尝试时间，供按仓库间隔调度使用（手动同步也会计入）
	lastAttemptMu.Lock()
	lastAttempt[repo.Path] = now
	lastAttemptMu.Unlock()

	result := gitSync(repo)

	// 失败或真正有动作时记录日志，让失败也能被界面看到
	if !result.Success || result.Committed || result.Pushed {
		saveSyncLog(db, types.SyncLog{
			RepoName:  repo.Name,
			RepoPath:  repo.Path,
			Time:      now.Format("2006-01-02 15:04:05"),
			Success:   result.Success,
			Message:   result.Message,
			CommitLog: result.CommitLog,
			PullLog:   result.PullLog,
			PushLog:   result.PushLog,
		})

		repo.LastSyncTime = now.Format("2006-01-02 15:04:05")
		updateRepoLastSyncTime(db, repo)
	}
	return result
}

func gitSync(repo types.GitRepo) types.SyncResult {
	if _, err := os.Stat(repo.Path); os.IsNotExist(err) {
		return types.SyncResult{
			Path:    repo.Path,
			Name:    repo.Name,
			Success: false,
			Message: "目录不存在",
		}
	}

	// 分支为空或 unborn（HEAD）时不能带 remote/branch 参数，否则 git pull/push 直接报错
	hasBranch := repo.Branch != "" && repo.Branch != "HEAD"

	// git add -A
	addOutput, addErr := gitcmd.Git(repo.Path, "add", "-A")
	result := types.SyncResult{
		Path:      repo.Path,
		Name:      repo.Name,
		CommitLog: addOutput,
	}
	if addErr != nil {
		result.Success = false
		result.Message = "添加文件失败"
		result.CommitLog += "\n错误: " + addOutput
		return result
	}

	// 检查是否有需要提交的更改
	statusOutput, _ := gitcmd.Git(repo.Path, "status", "--porcelain")
	hasChanges := len(strings.TrimSpace(statusOutput)) > 0

	if hasChanges {
		commitMsg := fmt.Sprintf("Sync: %s", time.Now().Format("2006-01-02 15:04:05"))
		commitOutput, commitErr := gitcmd.Git(repo.Path, "commit", "-m", commitMsg)
		result.CommitLog += "\n" + commitOutput
		if commitErr != nil {
			result.Success = false
			result.Message = "提交失败"
			result.CommitLog += "\n错误: " + commitOutput
			return result
		}
		result.Committed = true
	} else {
		result.CommitLog += "\n没有需要提交的更改"
	}

	// 如果是仅提交模式，跳过
	if repo.CommitOnly {
		result.Success = true
		result.Message = "同步完成"
		return result
	}

	// git pull
	pullArgs := []string{"pull"}
	if hasBranch && repo.Remote != "" {
		pullArgs = append(pullArgs, repo.Remote, repo.Branch)
	}
	pullOutput, pullErr := gitcmd.Git(repo.Path, pullArgs...)
	result.PullLog = pullOutput
	if pullErr != nil {
		result.PullLog += "\n错误: " + pullOutput
		result.Success = false
		result.Message = "拉取失败"
		return result
	}

	// 判断是否需要推送：本次有提交，或本地领先于远程分支（rev-list 不依赖 git 输出的语言）
	shouldPush := result.Committed
	if !shouldPush && hasBranch && repo.Remote != "" {
		upstream := repo.Remote + "/" + repo.Branch
		if refOut, refErr := gitcmd.Git(repo.Path, "rev-parse", "--verify", "--quiet", upstream); refErr == nil && refOut != "" {
			if countOut, countErr := gitcmd.Git(repo.Path, "rev-list", "--count", upstream+"..HEAD"); countErr == nil {
				if ahead, _ := strconv.Atoi(strings.TrimSpace(countOut)); ahead > 0 {
					shouldPush = true
				}
			}
		}
	}

	if shouldPush {
		pushArgs := []string{"push"}
		if hasBranch && repo.Remote != "" {
			pushArgs = append(pushArgs, repo.Remote, repo.Branch)
		}
		pushOutput, pushErr := gitcmd.Git(repo.Path, pushArgs...)
		result.PushLog = pushOutput
		if pushErr != nil {
			result.PushLog += "\n错误: " + pushOutput
			result.Success = false
			result.Message = "推送失败"
			return result
		}
		result.Pushed = true
	} else {
		result.PushLog = "没有需要推送的更改"
	}

	result.Success = true
	result.Message = "同步完成"
	return result
}

func getRsaPrivateKeyPath() string {
	userDir, _ := os.UserHomeDir()
	return filepath.Join(userDir, ".ssh", "id_rsa")
}

func saveSyncLog(db *sql.DB, logEntry types.SyncLog) {
	_, err := db.Exec(`
		INSERT INTO sync_logs (repo_name, repo_path, time, success, message, commit_log, pull_log, push_log)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, logEntry.RepoName, logEntry.RepoPath, logEntry.Time, logEntry.Success, logEntry.Message, logEntry.CommitLog, logEntry.PullLog, logEntry.PushLog)
	if err != nil {
		log.Printf("保存同步日志失败: %v", err)
	}

	db.Exec(`DELETE FROM sync_logs WHERE id NOT IN (SELECT id FROM sync_logs ORDER BY time DESC LIMIT 100)`)
}

func updateRepoLastSyncTime(db *sql.DB, repo types.GitRepo) {
	result, err := db.Exec("UPDATE git_repos SET last_sync_time = ?, updated_at = CURRENT_TIMESTAMP WHERE path = ?", repo.LastSyncTime, repo.Path)
	if err != nil {
		log.Printf("更新仓库同步时间失败: %v", err)
		return
	}
	rowsAffected, _ := result.RowsAffected()
	log.Printf("更新仓库 %s 的同步时间: %s, 影响行数: %d", repo.Name, repo.LastSyncTime, rowsAffected)
}
