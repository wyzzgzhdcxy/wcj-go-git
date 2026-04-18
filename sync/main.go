package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"wcj-go-common/core"
	myUtil "wcj-go-common/utils"

	_ "modernc.org/sqlite"
)

// GitRepo Git仓库信息
type GitRepo struct {
	Path         string `json:"path"`
	Name         string `json:"name"`
	Branch       string `json:"branch"`
	Remote       string `json:"remote"`
	RemoteUrl    string `json:"remoteUrl"`
	LastSyncTime string `json:"lastSyncTime"`
	Status       string `json:"status"`
	Enabled      bool   `json:"enabled"`
	AutoSync     bool   `json:"autoSync"`
	CommitOnly   bool   `json:"commitOnly"`
}

// GitSyncResult 单个仓库同步结果
type GitSyncResult struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	PullLog   string `json:"pullLog"`
	PushLog   string `json:"pushLog"`
	CommitLog string `json:"commitLog"`
	Committed bool   `json:"committed"`
	Pushed    bool   `json:"pushed"`
}

// GitSyncLog 同步日志
type GitSyncLog struct {
	RepoName  string
	RepoPath  string
	Time      string
	Success   bool
	Message   string
	CommitLog string
	PullLog   string
	PushLog   string
}

// autoSyncTicker 自动同步定时器
var autoSyncTicker *time.Ticker
var autoSyncRunning bool
var reposCache []GitRepo
var reposMu sync.RWMutex

func main() {
	myUtil.InitLog(true)
	startSt := time.Now().Format("2006-01-02 15:04:05.000")
	core.MkDirALl0755(filepath.Join(core.GetTempDir(), "/codeGen"))
	log.Printf("%s", startSt+" log init finish! "+time.Now().Format("2006-01-02 15:04:05.000"))

	// 初始化数据库
	db, err := initDb()
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer db.Close()

	// 初始加载仓库缓存
	reposCache = loadEnabledRepos(db)

	// 启动自动同步
	startAutoSync(db)

	// 启动 HTTP 更新接口（阻塞主线程）
	startHttpServer(db)
}

// startHttpServer 启动 HTTP 服务器，监听更新请求
func startHttpServer(db *sql.DB) {
	http.HandleFunc("/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		repos := loadEnabledRepos(db)
		reposMu.Lock()
		reposCache = repos
		reposMu.Unlock()
		log.Printf("收到刷新请求，已更新缓存，共 %d 个仓库", len(repos))
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK: %d repos refreshed", len(repos))
	})

	// 手动同步接口
	http.HandleFunc("/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// 从缓存获取仓库列表进行同步
		reposMu.RLock()
		repos := reposCache
		reposMu.RUnlock()
		for _, repo := range repos {
			doSync(db, repo)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK: %d repos synced", len(repos))
	})

	// 信号处理，优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Println("HTTP 接口已启动: http://localhost:9090")

	go func() {
		if err := http.ListenAndServe(":9090", nil); err != nil {
			log.Printf("HTTP 服务器退出: %v", err)
		}
	}()

	<-quit
	log.Println("收到退出信号，正在停止...")
	if autoSyncTicker != nil {
		autoSyncTicker.Stop()
	}
	os.Exit(0)
}

func initDb() (*sql.DB, error) {
	dbPath := core.GetTempDir() + "/data/sync_list.db"
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %v", err)
	}

	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("数据库连接失败: %v", err)
	}

	return db, nil
}

func loadEnabledRepos(db *sql.DB) []GitRepo {
	rows, err := db.Query(`
		SELECT path, name, branch, remote, remote_url, last_sync_time, status, enabled, auto_sync, commit_only
		FROM git_repos WHERE enabled = 1 AND (auto_sync = 1 OR commit_only = 1) ORDER BY id
	`)
	if err != nil {
		log.Printf("查询仓库失败: %v", err)
		return nil
	}
	defer rows.Close()

	var repos []GitRepo
	for rows.Next() {
		var repo GitRepo
		var enabled, autoSync, commitOnly int
		if err := rows.Scan(&repo.Path, &repo.Name, &repo.Branch, &repo.Remote, &repo.RemoteUrl, &repo.LastSyncTime, &repo.Status, &enabled, &autoSync, &commitOnly); err != nil {
			continue
		}
		repo.Enabled = enabled == 1
		repo.AutoSync = autoSync == 1
		repo.CommitOnly = commitOnly == 1
		repos = append(repos, repo)
	}
	return repos
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
		for range autoSyncTicker.C {
			log.Printf("轮询定时任务触发")
			reposMu.RLock()
			repos := reposCache
			reposMu.RUnlock()
			for _, repo := range repos {
				doSync(db, repo)
			}
		}
	}()
}

func doSync(db *sql.DB, repo GitRepo) {
	now := time.Now()
	result := gitSync(repo)

	if len(result) > 0 {
		syncResult := result[0]
		if syncResult.Committed || syncResult.Pushed {
			saveSyncLog(db, GitSyncLog{
				RepoName:  repo.Name,
				RepoPath:  repo.Path,
				Time:      now.Format("2006-01-02 15:04:05"),
				Success:   syncResult.Success,
				Message:   syncResult.Message,
				CommitLog: syncResult.CommitLog,
				PullLog:   syncResult.PullLog,
				PushLog:   syncResult.PushLog,
			})

			repo.LastSyncTime = now.Format("2006-01-02 15:04:05")
			updateRepoLastSyncTime(db, repo)
		}
	}
}

func gitSync(repo GitRepo) []GitSyncResult {
	results := make([]GitSyncResult, 0)

	if _, err := os.Stat(repo.Path); os.IsNotExist(err) {
		results = append(results, GitSyncResult{
			Path:    repo.Path,
			Name:    repo.Name,
			Success: false,
			Message: "目录不存在",
		})
		return results
	}

	// git add .
	addOutput, addErr := runWithDirAndOutput(repo.Path, "git", "add", "-A")
	result := GitSyncResult{
		Path:      repo.Path,
		Name:      repo.Name,
		CommitLog: string(addOutput),
	}
	if addErr != nil {
		result.CommitLog += "\n错误: " + addErr.Error()
	}

	// 检查是否有需要提交的更改
	statusOutput, _ := runWithDirAndOutput(repo.Path, "git", "status", "--porcelain")
	hasChanges := len(strings.TrimSpace(string(statusOutput))) > 0

	if hasChanges {
		commitMsg := fmt.Sprintf("Sync: %s", time.Now().Format("2006-01-02 15:04:05"))
		commitOutput, commitErr := runWithDirAndOutput(repo.Path, "git", "commit", "-m", commitMsg)
		result.CommitLog += "\n" + string(commitOutput)
		if commitErr != nil {
			result.CommitLog += "\n错误: " + commitErr.Error()
		} else {
			result.Committed = true
		}
	} else {
		result.CommitLog += "\n没有需要提交的更改"
	}

	// 如果是仅提交模式，跳过
	if repo.CommitOnly {
		result.Success = true
		result.Message = "同步完成"
		results = append(results, result)
		return results
	}

	// git pull
	pullOutput, pullErr := runWithDirAndOutput(repo.Path, "git", "pull", repo.Remote, repo.Branch)
	result.PullLog = string(pullOutput)
	if pullErr != nil {
		result.PullLog += "\n错误: " + pullErr.Error()
	}

	// 检查是否需要推送
	shouldPush := result.Committed
	if !shouldPush {
		pushStatusOutput, _ := runWithDirAndOutput(repo.Path, "git", "status", "--porcelain")
		shouldPush = len(strings.TrimSpace(string(pushStatusOutput))) > 0
	}
	if !shouldPush {
		statusFullOutput, _ := runWithDirAndOutput(repo.Path, "git", "status")
		shouldPush = strings.Contains(string(statusFullOutput), "Your branch is ahead")
	}

	if shouldPush {
		pushOutput, pushErr := runWithDirAndOutput(repo.Path, "git", "push", repo.Remote, repo.Branch)
		result.PushLog = string(pushOutput)
		if pushErr != nil {
			result.PushLog += "\n错误: " + pushErr.Error()
		} else {
			result.Pushed = true
		}
	} else {
		result.PushLog = "没有需要推送的更改"
	}

	result.Success = true
	result.Message = "同步完成"
	results = append(results, result)

	return results
}

func runWithDirAndOutput(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_SSH_COMMAND=ssh -i "+getRsaPrivateKeyPath())
	// Windows 下隐藏命令行窗口
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func getRsaPrivateKeyPath() string {
	userDir, _ := os.UserHomeDir()
	return userDir + "\\.ssh\\id_rsa"
}

func saveSyncLog(db *sql.DB, logEntry GitSyncLog) {
	_, err := db.Exec(`
		INSERT INTO sync_logs (repo_name, repo_path, time, success, message, commit_log, pull_log, push_log)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, logEntry.RepoName, logEntry.RepoPath, logEntry.Time, logEntry.Success, logEntry.Message, logEntry.CommitLog, logEntry.PullLog, logEntry.PushLog)
	if err != nil {
		log.Printf("保存同步日志失败: %v", err)
	}

	db.Exec(`DELETE FROM sync_logs WHERE id NOT IN (SELECT id FROM sync_logs ORDER BY time DESC LIMIT 100)`)
}

func updateRepoLastSyncTime(db *sql.DB, repo GitRepo) {
	result, err := db.Exec("UPDATE git_repos SET last_sync_time = ?, updated_at = CURRENT_TIMESTAMP WHERE path = ?", repo.LastSyncTime, repo.Path)
	if err != nil {
		log.Printf("更新仓库同步时间失败: %v", err)
		return
	}
	rowsAffected, _ := result.RowsAffected()
	log.Printf("更新仓库 %s 的同步时间: %s, 影响行数: %d", repo.Name, repo.LastSyncTime, rowsAffected)
}
