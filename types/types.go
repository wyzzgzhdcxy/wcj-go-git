// Package types 定义 GUI 与 sync 后台服务之间共享的数据结构与接口约定。
// 后台服务是数据库唯一属主，GUI 通过 HTTP 接口访问数据，两侧统一引用本包，避免结构体重复定义。
package types

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
	CommitOnly      bool   `json:"commitOnly"`      // 仅提交，不推送
	Interval        int    `json:"interval"`        // 自动同步间隔（分钟），0=每次轮询（每分钟）
	LastSyncSuccess int    `json:"lastSyncSuccess"` // 最近同步状态: -1=从未同步, 0=失败, 1=成功
}

// RepoListReq 仓库列表保存请求
type RepoListReq struct {
	Repos []GitRepo `json:"repos"`
}

// RepoListRes 仓库列表结果
type RepoListRes struct {
	Success bool      `json:"success"`
	Message string    `json:"message"`
	Repos   []GitRepo `json:"repos"`
}

// SyncReq 手动同步请求（服务端按数据库中已启用仓库执行，请求体仅为兼容保留）
type SyncReq struct {
	Repos []GitRepo `json:"repos"`
}

// SyncResult 单个仓库同步结果
type SyncResult struct {
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

// SyncRes 同步结果
type SyncRes struct {
	Success bool         `json:"success"`
	Message string       `json:"message"`
	Results []SyncResult `json:"results"`
}

// SyncLog 同步日志
type SyncLog struct {
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

// SyncLogsReq 同步日志查询请求
type SyncLogsReq struct {
	RepoPath string `json:"repoPath"` // 仓库路径(可选)
	Limit    int    `json:"limit"`    // 获取条数
}

// SyncLogsRes 同步日志查询结果
type SyncLogsRes struct {
	Success bool      `json:"success"`
	Message string    `json:"message"`
	Logs    []SyncLog `json:"logs"`
}

// KeyValueReq 配置读写请求（key 必填，set 时 value 必填）
type KeyValueReq struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// KeyValueRes 配置读写结果（get 返回 value，set/delete 返回 message）
type KeyValueRes struct {
	Success bool   `json:"success"`
	Value   string `json:"value"`
	Message string `json:"message"`
}

// FingerprintRes 数据库内容指纹，GUI 用它检测数据是否变化
type FingerprintRes struct {
	Fingerprint string `json:"fingerprint"`
}
