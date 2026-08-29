package main

import (
	"path/filepath"
	"testing"
	"time"

	"wcj-go-git/types"

	_ "modernc.org/sqlite"
)

// isolateCacheDir 把用户缓存目录指到临时目录，保证测试不触碰真实数据库
func isolateCacheDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("LocalAppData", tmp)                         // Windows
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "xdg")) // Linux/macOS
}

// TestInitDbAndRepoRoundTrip 验证建库、仓库保存/加载（含间隔字段）与启用过滤
func TestInitDbAndRepoRoundTrip(t *testing.T) {
	isolateCacheDir(t)
	db, err := initDb()
	if err != nil {
		t.Fatalf("initDb 失败: %v", err)
	}
	defer db.Close()

	repos := []types.GitRepo{
		{Path: "C:/repos/a", Name: "a", Branch: "main", Remote: "origin", Enabled: true, AutoSync: true, Interval: 30},
		{Path: "C:/repos/b", Name: "b", Branch: "dev", Remote: "origin", Enabled: true, CommitOnly: true},
		{Path: "C:/repos/c", Name: "c", Enabled: false},
	}
	if err := saveRepos(db, repos); err != nil {
		t.Fatalf("saveRepos 失败: %v", err)
	}

	all := loadAllRepos(db)
	if len(all) != 3 {
		t.Fatalf("期望 3 个仓库, 实际 %d", len(all))
	}
	if all[0].Interval != 30 || !all[0].AutoSync {
		t.Errorf("间隔/自动同步字段未正确保存: %+v", all[0])
	}
	if all[2].LastSyncSuccess != -1 {
		t.Errorf("从未同步的仓库应为 -1, 实际 %d", all[2].LastSyncSuccess)
	}

	if got := len(loadEnabledRepos(db)); got != 2 {
		t.Errorf("启用仓库期望 2 个, 实际 %d", got)
	}
	autoRepos := loadAutoSyncRepos(db)
	if len(autoRepos) != 2 {
		t.Errorf("参与自动同步的仓库期望 2 个, 实际 %d", len(autoRepos))
	}
}

// TestMigrationFromLegacySchema 模拟旧版数据库（无 sync_interval 等列），验证迁移后可正常读写
func TestMigrationFromLegacySchema(t *testing.T) {
	isolateCacheDir(t)

	// 用旧版表结构预置一个数据库
	db, err := initDb()
	if err != nil {
		t.Fatalf("initDb 失败: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE legacy_repos AS SELECT * FROM git_repos WHERE 0
	`); err != nil {
		t.Fatalf("创建影子表失败: %v", err)
	}
	db.Close()

	// 重新初始化应幂等通过（CREATE IF NOT EXISTS + duplicate column 容错）
	db2, err := initDb()
	if err != nil {
		t.Fatalf("二次 initDb 失败: %v", err)
	}
	defer db2.Close()

	if err := saveRepos(db2, []types.GitRepo{{Path: "C:/repos/x", Name: "x", Enabled: true}}); err != nil {
		t.Fatalf("迁移后保存失败: %v", err)
	}
}

// TestConfigAndLogsAndFingerprint 验证配置读写、日志查询与指纹变化
func TestConfigAndLogsAndFingerprint(t *testing.T) {
	isolateCacheDir(t)
	db, err := initDb()
	if err != nil {
		t.Fatalf("initDb 失败: %v", err)
	}
	defer db.Close()

	// 配置
	if err := setConfig(db, "k1", "v1"); err != nil {
		t.Fatalf("setConfig 失败: %v", err)
	}
	if v, _ := getConfig(db, "k1"); v != "v1" {
		t.Errorf("配置读取期望 v1, 实际 %q", v)
	}
	if err := setConfig(db, "k1", "v2"); err != nil {
		t.Fatalf("覆盖配置失败: %v", err)
	}
	if v, _ := getConfig(db, "k1"); v != "v2" {
		t.Errorf("配置覆盖期望 v2, 实际 %q", v)
	}
	if v, _ := getConfig(db, "missing"); v != "" {
		t.Errorf("缺失配置应返回空串, 实际 %q", v)
	}
	_ = deleteConfig(db, "k1")
	if v, _ := getConfig(db, "k1"); v != "" {
		t.Errorf("删除后应返回空串, 实际 %q", v)
	}

	fpBefore := dbFingerprint(db)
	saveSyncLog(db, types.SyncLog{
		RepoName: "a", RepoPath: "C:/repos/a", Time: time.Now().Format("2006-01-02 15:04:05"),
		Success: false, Message: "推送失败", CommitLog: "add", PullLog: "pull", PushLog: "push",
	})
	fpAfter := dbFingerprint(db)
	if fpBefore == fpAfter || fpAfter == "" {
		t.Errorf("写日志后指纹应变化: before=%q after=%q", fpBefore, fpAfter)
	}

	logsRes := queryLogs(db, types.SyncLogsReq{Limit: 10})
	if !logsRes.Success || len(logsRes.Logs) != 1 {
		t.Fatalf("日志查询异常: %+v", logsRes)
	}
	if logsRes.Logs[0].Success || logsRes.Logs[0].Message != "推送失败" {
		t.Errorf("日志内容不符: %+v", logsRes.Logs[0])
	}
}

// TestDueForSync 验证按仓库间隔调度
func TestDueForSync(t *testing.T) {
	lastAttempt = make(map[string]time.Time)
	now := time.Now()

	if !dueForSync(types.GitRepo{Path: "p0"}, now) {
		t.Error("interval=0 应每次都同步")
	}
	if !dueForSync(types.GitRepo{Path: "p60", Interval: 60}, now) {
		t.Error("首次出现的仓库应立即同步")
	}
	lastAttempt["p60"] = now.Add(-10 * time.Minute)
	if dueForSync(types.GitRepo{Path: "p60", Interval: 60}, now) {
		t.Error("间隔未到不应同步")
	}
	lastAttempt["p60"] = now.Add(-61 * time.Minute)
	if !dueForSync(types.GitRepo{Path: "p60", Interval: 60}, now) {
		t.Error("超过间隔应同步")
	}
}
