package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"wcj-go-git/gitcmd"
	"wcj-go-git/types"

	"github.com/wyzzgzhdcxy/wcj-go-common/core"
	myUtil "github.com/wyzzgzhdcxy/wcj-go-common/utils"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct（界面进程不访问数据库：领域数据全部经 HTTP 代理到后台 sync 服务，
// 窗口状态等 UI 状态保存在本地 JSON 文件）
type App struct {
	ctx    context.Context
	Assets embed.FS
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
	// 启动数据变化监听，把同步日志/仓库列表的变化以事件推送给前端（取代前端定时轮询）
	a.StartChangeWatcher(ctx)
}

// ==================== 后台服务访问 ====================

// svcBaseAddr 后台 sync 服务地址（仅监听本机回环）
const svcBaseAddr = "http://127.0.0.1:19090"

// svcPost 调用后台服务接口；服务未监听时先尝试自动启动再重试一次。
// 所有接口都返回非空 JSON，空串即代表调用失败。
func svcPost(path string, reqBody string) (string, error) {
	body := myUtil.HttpPostJson(svcBaseAddr+path, reqBody)
	if body != "" {
		return body, nil
	}
	ensureSyncService()
	body = myUtil.HttpPostJson(svcBaseAddr+path, reqBody)
	if body == "" {
		return "", fmt.Errorf("同步服务未响应，请确认 sync 服务已启动")
	}
	return body, nil
}

// ensureSyncService 确保后台同步服务已启动：服务未监听时尝试启动同目录下的 sync.exe
func ensureSyncService() {
	const syncAddr = "127.0.0.1:19090"
	if conn, err := net.DialTimeout("tcp", syncAddr, 300*time.Millisecond); err == nil {
		conn.Close()
		return
	}

	exePath, err := os.Executable()
	if err != nil {
		return
	}
	syncExe := filepath.Join(filepath.Dir(exePath), "sync.exe")
	if _, err := os.Stat(syncExe); err != nil {
		log.Printf("未找到同步服务 %s，跳过自动启动", syncExe)
		return
	}

	cmd := exec.Command(syncExe)
	core.SetHideWindow(cmd)
	if err := cmd.Start(); err != nil {
		log.Printf("启动同步服务失败: %v", err)
		return
	}
	// 等待端口就绪（最多约 2 秒）
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		conn, err := net.DialTimeout("tcp", syncAddr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			log.Printf("同步服务已自动启动")
			return
		}
	}
	log.Printf("同步服务启动超时")
}

// StartChangeWatcher 周期性比对新后台服务的数据库指纹，数据变化时通过 EventsEmit 推送给前端。
// 数据库由服务独占，GUI 不直接访问数据库，只轮询一条轻量指纹接口；
// 指纹变化时才拉取全量数据并向 WebView 发事件。
func (a *App) StartChangeWatcher(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		var lastFingerprint string
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				body, err := svcPost("/fingerprint", "")
				if err != nil {
					continue
				}
				var fp types.FingerprintRes
				if json.Unmarshal([]byte(body), &fp) != nil || fp.Fingerprint == "" {
					continue
				}
				if fp.Fingerprint == lastFingerprint {
					continue
				}
				// 首次运行只记录基线不推送：前端启动时会主动加载一次
				if lastFingerprint == "" {
					lastFingerprint = fp.Fingerprint
					continue
				}
				lastFingerprint = fp.Fingerprint

				if logsRes := a.GetSyncLogs(types.SyncLogsReq{Limit: 50}); logsRes.Success {
					runtime.EventsEmit(ctx, "sync:logs", logsRes.Logs)
				}
				if reposRes := a.LoadGitRepoList(); reposRes.Success {
					runtime.EventsEmit(ctx, "git:repos", reposRes.Repos)
				}
			}
		}
	}()
}

// ==================== 窗口状态（本地 JSON 文件，不经数据库） ====================

// uiStateFilePath UI 状态文件路径
func uiStateFilePath() string {
	return filepath.Join(core.GetTempDir(), "ui_state.json")
}

// WindowState 窗口状态
type WindowState struct {
	Width     int `json:"width"`
	Height    int `json:"height"`
	X         int `json:"x"`
	Y         int `json:"y"`
	Maximized int `json:"maximized"`
}

// GetWindowState 获取窗口状态（文件不存在或损坏时返回零值，由调用方使用默认尺寸）
func (a *App) GetWindowState() WindowState {
	var ws WindowState
	data := core.ReadFileToByte(uiStateFilePath())
	if len(data) > 0 {
		core.JsonToObject(&data, &ws)
	}
	return ws
}

// SaveWindowState 保存窗口状态
func (a *App) SaveWindowState(ws WindowState) error {
	core.WriteStrToFile(uiStateFilePath(), core.ToJsonString(ws))
	if exists, _ := core.PathExists(uiStateFilePath()); !exists {
		return fmt.Errorf("写入 UI 状态文件失败: %s", uiStateFilePath())
	}
	return nil
}

// SaveCurrentWindowState 捕获并保存当前窗口状态
func (a *App) SaveCurrentWindowState() error {
	if a.ctx == nil {
		return fmt.Errorf("context未初始化")
	}
	maximized := 0
	if runtime.WindowIsMaximised(a.ctx) {
		maximized = 1
	}
	if maximized == 1 {
		// 最大化状态下取到的宽高是铺满屏幕的尺寸，直接保存会导致还原后窗口占满整个屏幕；
		// 保留上一次正常状态下的几何信息，仅更新最大化标记
		prev := a.GetWindowState()
		if prev.Width > 0 && prev.Height > 0 {
			return a.SaveWindowState(WindowState{
				Width:     prev.Width,
				Height:    prev.Height,
				X:         prev.X,
				Y:         prev.Y,
				Maximized: 1,
			})
		}
	}
	width, height := runtime.WindowGetSize(a.ctx)
	x, y := runtime.WindowGetPosition(a.ctx)
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

// ==================== Git同步功能（数据经后台服务） ====================

// GitSync 同步Git仓库（调用后台 sync 服务，由其逐仓库执行并返回每个仓库的结果）
func (a *App) GitSync(req types.SyncReq) types.SyncRes {
	body, err := svcPost("/sync", "")
	if err != nil {
		return types.SyncRes{
			Success: false,
			Message: err.Error(),
			Results: []types.SyncResult{},
		}
	}

	var res types.SyncRes
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		return types.SyncRes{
			Success: false,
			Message: "解析同步结果失败: " + err.Error(),
			Results: []types.SyncResult{},
		}
	}

	successCount := 0
	for _, r := range res.Results {
		if r.Success {
			successCount++
		}
	}
	return types.SyncRes{
		Success: true,
		Message: fmt.Sprintf("同步完成：%d 成功 / %d 失败", successCount, len(res.Results)-successCount),
		Results: res.Results,
	}
}

// GetGitRepoInfo 获取Git仓库信息
type GetGitRepoInfoReq struct {
	Path string `json:"path"` // 仓库路径
}

// GetGitRepoInfoRes 获取仓库信息结果
type GetGitRepoInfoRes struct {
	Success   bool           `json:"success"`
	Message   string         `json:"message"`
	Repo      *types.GitRepo `json:"repo"`
	IsGitRepo bool           `json:"isGitRepo"` // 是否是git仓库
}

// GetGitRepoInfo 获取Git仓库信息（只读 git 命令检查，不访问数据库）
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

	// 获取当前分支（ unborn 仓库或命令失败时返回空，由界面显示空分支）
	branch, branchErr := gitcmd.Git(req.Path, "rev-parse", "--abbrev-ref", "HEAD")
	if branchErr != nil || branch == "" || branch == "HEAD" {
		branch = ""
	}

	// 获取远程仓库信息
	remoteUrl, remoteErr := gitcmd.Git(req.Path, "remote", "get-url", "origin")
	if remoteErr != nil {
		remoteUrl = ""
	}

	// 获取仓库名称
	repoName := filepath.Base(req.Path)

	repo := &types.GitRepo{
		Path:      req.Path,
		Name:      repoName,
		Branch:    branch,
		Remote:    "origin",
		RemoteUrl: remoteUrl,
		Status:    "就绪",
		Enabled:   true,
		Interval:  0,
		// 新添加的仓库从未同步过，置为 -1（否则 0 会被界面当作"同步失败"）
		LastSyncSuccess: -1,
	}

	return GetGitRepoInfoRes{
		Success:   true,
		Message:   "获取成功",
		Repo:      repo,
		IsGitRepo: true,
	}
}

// SaveGitRepoList 保存仓库列表（代理到后台服务，由其写库并刷新自动同步缓存）
func (a *App) SaveGitRepoList(req types.RepoListReq) types.RepoListRes {
	body, err := svcPost("/repos/save", core.ToJsonString(req))
	if err != nil {
		return types.RepoListRes{Success: false, Message: err.Error()}
	}
	var res types.RepoListRes
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		return types.RepoListRes{Success: false, Message: "解析保存结果失败: " + err.Error()}
	}
	return res
}

// LoadGitRepoList 加载仓库列表（代理到后台服务）
func (a *App) LoadGitRepoList() types.RepoListRes {
	body, err := svcPost("/repos/list", "")
	if err != nil {
		return types.RepoListRes{Success: false, Message: err.Error(), Repos: []types.GitRepo{}}
	}
	var res types.RepoListRes
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		return types.RepoListRes{Success: false, Message: "解析仓库列表失败: " + err.Error(), Repos: []types.GitRepo{}}
	}
	return res
}

// GetSyncLogs 获取同步日志（代理到后台服务）
func (a *App) GetSyncLogs(req types.SyncLogsReq) types.SyncLogsRes {
	body, err := svcPost("/logs", core.ToJsonString(req))
	if err != nil {
		return types.SyncLogsRes{Success: false, Message: err.Error(), Logs: []types.SyncLog{}}
	}
	var res types.SyncLogsRes
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		return types.SyncLogsRes{Success: false, Message: "解析日志失败: " + err.Error(), Logs: []types.SyncLog{}}
	}
	return res
}

// SendToFrontend 发送消息到前端
func (a *App) SendToFrontend(event string, data interface{}) {
	runtime.EventsEmit(a.ctx, event, data)
}

// CopyToClipboard 复制到剪贴板
func (a *App) CopyToClipboard(text string) error {
	return runtime.ClipboardSetText(a.ctx, text)
}

// ==================== 配置（settings 表由后台服务独占） ====================

// GetConfig 获取配置
func (a *App) GetConfig(key string) (string, error) {
	body, err := svcPost("/config/get", core.ToJsonString(types.KeyValueReq{Key: key}))
	if err != nil {
		return "", err
	}
	var res types.KeyValueRes
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		return "", err
	}
	if !res.Success {
		return "", fmt.Errorf("%s", res.Message)
	}
	return res.Value, nil
}

// SetConfig 设置配置
func (a *App) SetConfig(key, value string) error {
	body, err := svcPost("/config/set", core.ToJsonString(types.KeyValueReq{Key: key, Value: value}))
	if err != nil {
		return err
	}
	var res types.KeyValueRes
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		return err
	}
	if !res.Success {
		return fmt.Errorf("%s", res.Message)
	}
	return nil
}

// DeleteConfig 删除配置
func (a *App) DeleteConfig(key string) error {
	body, err := svcPost("/config/delete", core.ToJsonString(types.KeyValueReq{Key: key}))
	if err != nil {
		return err
	}
	var res types.KeyValueRes
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		return err
	}
	if !res.Success {
		return fmt.Errorf("%s", res.Message)
	}
	return nil
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
	for _, name := range core.ListFileName(dir, "", false, true) {
		if strings.HasSuffix(name, ".env") || strings.HasSuffix(name, ".properties") {
			envFiles = append(envFiles, filepath.Join(dir, name))
		}
	}
	return envFiles, nil
}

// ReadFile 读取文件
func (a *App) ReadFile(path string) (string, error) {
	if exists, _ := core.PathExists(path); !exists {
		return "", fmt.Errorf("文件不存在: %s", path)
	}
	content := core.ReadFileToStr(path)
	return content, nil
}

// WriteFile 写入文件
func (a *App) WriteFile(path, content string) error {
	core.WriteStrToFile(path, content)
	if exists, _ := core.PathExists(path); !exists {
		return fmt.Errorf("写入文件失败: %s", path)
	}
	return nil
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
	core.MkDirALl0755(path)
	if exists, _ := core.PathExists(path); !exists {
		return fmt.Errorf("创建目录失败: %s", path)
	}
	return nil
}

// ListDir 列出目录
func (a *App) ListDir(dir string) ([]string, error) {
	names := core.ListFileName(dir, "", true, true)
	if len(names) == 0 {
		if exists, _ := core.PathExists(dir); !exists {
			return nil, fmt.Errorf("目录不存在: %s", dir)
		}
	}
	return names, nil
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

	// 删除 .git 前必须先拿到原分支名与远程地址
	branchOut, branchErr := gitcmd.Git(projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	branch := strings.TrimSpace(branchOut)
	if branchErr != nil || branch == "" || branch == "HEAD" {
		branch = "master"
		log.Printf("获取分支名失败，使用默认值 master")
	}
	log.Printf("检测到分支: %s", branch)

	remoteURL, remoteErr := gitcmd.Git(projectDir, "remote", "get-url", "origin")
	remoteURL = strings.TrimSpace(remoteURL)
	if remoteErr != nil || remoteURL == "" {
		log.Printf("获取远程地址失败: %s", remoteURL)
		return ResetResult{
			Success: false,
			Message: "未检测到远程地址，请确保仓库已配置 remote origin",
			Output:  remoteURL,
		}
	}
	log.Printf("检测到远程地址: %s", remoteURL)

	gitDir := filepath.Join(projectDir, ".git")

	var output string

	// 1. rm -rf .git (使用 Go 实现)，失败则中止
	output += "rm -rf .git\n"
	if err := os.RemoveAll(gitDir); err != nil {
		output += err.Error() + "\n"
		log.Printf("删除 .git 失败: %v", err)
		return ResetResult{
			Success: false,
			Message: "删除 .git 失败",
			Output:  output,
		}
	}

	// 2. git init -b <branch>
	initOut, initErr := gitcmd.Git(projectDir, "init", "-b", branch)
	output += fmt.Sprintf("git init -b %s\n%s\n", branch, initOut)
	if initErr != nil {
		return ResetResult{
			Success: false,
			Message: "git init 失败",
			Output:  output,
		}
	}

	// 3. git add .
	addOut, addErr := gitcmd.Git(projectDir, "add", ".")
	output += fmt.Sprintf("git add .\n%s\n", addOut)
	if addErr != nil {
		return ResetResult{
			Success: false,
			Message: "git add 失败",
			Output:  output,
		}
	}

	// 4. git commit -m "基本功能实现V1.0"
	commitOut, commitErr := gitcmd.Git(projectDir, "commit", "-m", "基本功能实现V1.0")
	output += fmt.Sprintf("git commit -m \"基本功能实现V1.0\"\n%s\n", commitOut)
	if commitErr != nil {
		return ResetResult{
			Success: false,
			Message: "git commit 失败",
			Output:  output,
		}
	}

	// 5. git remote add origin <url>（正常情况下新 init 的仓库不会有旧 remote，set-url 仅作兜底）
	remoteAddOut, remoteAddErr := gitcmd.Git(projectDir, "remote", "add", "origin", remoteURL)
	output += fmt.Sprintf("git remote add origin %s\n%s\n", remoteURL, remoteAddOut)
	if remoteAddErr != nil {
		setUrlOut, setUrlErr := gitcmd.Git(projectDir, "remote", "set-url", "origin", remoteURL)
		output += fmt.Sprintf("git remote set-url origin %s\n%s\n", remoteURL, setUrlOut)
		if setUrlErr != nil {
			return ResetResult{
				Success: false,
				Message: "设置远程地址失败",
				Output:  output,
			}
		}
	}

	// 6. git push -f -u origin <branch>
	pushOut, pushErr := gitcmd.Git(projectDir, "push", "-f", "-u", "origin", branch)
	output += fmt.Sprintf("git push -f -u origin %s\n%s\n", branch, pushOut)
	if pushErr != nil {
		return ResetResult{
			Success: false,
			Message: "git push 失败",
			Output:  output,
		}
	}

	log.Printf("重置完成, 输出:\n%s", output)
	return ResetResult{
		Success: true,
		Message: "重置完成",
		Output:  output,
	}
}

// SoftResetResult 软重置结果
type SoftResetResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Output  string `json:"output"`
}

// SoftResetReq 软重置请求
type SoftResetReq struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// SoftReset 将未推送到远程的提交合并为一次提交
func (a *App) SoftReset(req SoftResetReq) SoftResetResult {
	projectDir := req.Path
	commitMsg := strings.TrimSpace(req.Message)
	if commitMsg == "" {
		commitMsg = "合并本地未推送的提交"
	}
	log.Printf("开始软重置, 目录: %s, 提交信息: %s", projectDir, commitMsg)

	var output string

	branchOut, branchErr := gitcmd.Git(projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	if branchErr != nil || branchOut == "" || branchOut == "HEAD" {
		return SoftResetResult{
			Success: false,
			Message: "未检测到当前分支，请确保是有效的 Git 仓库",
			Output:  branchOut,
		}
	}
	branch := branchOut

	output += fmt.Sprintf("当前分支: %s\n", branch)

	fetchOut, fetchErr := gitcmd.Git(projectDir, "fetch", "origin", branch)
	output += fmt.Sprintf("git fetch origin %s\n%s\n", branch, fetchOut)
	if fetchErr != nil {
		log.Printf("git fetch 失败: %s", fetchOut)
	}

	remoteRef := fmt.Sprintf("origin/%s", branch)
	existsOut, existsErr := gitcmd.Git(projectDir, "rev-parse", "--verify", "--quiet", remoteRef)
	if existsErr != nil || existsOut == "" {
		return SoftResetResult{
			Success: false,
			Message: fmt.Sprintf("未找到远程分支 %s，无法软重置", remoteRef),
			Output:  output,
		}
	}

	resetOut, resetErr := gitcmd.Git(projectDir, "reset", "--soft", remoteRef)
	output += fmt.Sprintf("git reset --soft %s\n%s\n", remoteRef, resetOut)
	if resetErr != nil {
		log.Printf("git reset --soft 失败: %s", resetOut)
		return SoftResetResult{
			Success: false,
			Message: "软重置失败，请确认本地有未推送的提交",
			Output:  output,
		}
	}

	commitOut, commitErr := gitcmd.Git(projectDir, "commit", "-m", commitMsg)
	output += fmt.Sprintf("git commit -m \"%s\"\n%s\n", commitMsg, commitOut)
	if commitErr != nil {
		log.Printf("git commit 失败: %s", commitOut)
		return SoftResetResult{
			Success: false,
			Message: "提交失败，可能没有可提交的内容",
			Output:  output,
		}
	}

	log.Printf("软重置完成, 输出:\n%s", output)
	return SoftResetResult{
		Success: true,
		Message: "合并成功",
		Output:  output,
	}
}

// PackageResult 打包结果
type PackageResult struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	Output    string `json:"output"`
	OutputDir string `json:"outputDir"`
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

	log.Printf("执行命令: wails build, 工作目录: %s", projectDir)
	outputBytes := core.ExecuteCommandByTargetDir(projectDir, "wails", "build")
	output := string(*outputBytes)
	log.Printf("打包输出:\n%s", output)

	if strings.Contains(output, "ERROR") || strings.Contains(output, "Error:") {
		log.Printf("打包失败: %s", output)
		return PackageResult{
			Success:   false,
			Message:   "打包失败",
			Output:    output,
			OutputDir: "",
		}
	}

	// 打包产物在 projectDir/build/bin/ 目录下
	outputDir := filepath.Join(projectDir, "build", "bin")

	// 查找最新生成的 exe 文件（目录中可能残留历史构建产物）
	var exeFile string
	var newest time.Time
	entries, err := os.ReadDir(outputDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".exe") {
				continue
			}
			info, infoErr := entry.Info()
			if infoErr == nil && (exeFile == "" || info.ModTime().After(newest)) {
				exeFile = entry.Name()
				newest = info.ModTime()
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
	targetDir := getPackageTargetDir()
	targetPath := filepath.Join(targetDir, exeFile)

	// 确保目标目录存在
	core.MkDirALl0755(targetDir)

	// 复制文件（common 的 CopyFile 打开目标文件不带 O_TRUNC，先删除旧文件避免残留字节导致 exe 损坏）
	sourcePath := filepath.Join(outputDir, exeFile)
	_ = os.Remove(targetPath)
	if _, err := core.CopyFile(sourcePath, targetPath); err != nil {
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
