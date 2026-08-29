// Package gitcmd 提供带真实退出码的命令执行封装。
// common 的 core.ExecuteCommandByTargetDir 只返回输出、不返回 error，
// 而 git 命令失败时（push 被拒、合并冲突等）输出并不都以 fatal:/error: 开头，
// 仅凭输出前缀无法可靠判断成败，因此提供本封装；隐藏窗口能力复用 common 的 core.SetHideWindow。
package gitcmd

import (
	"os/exec"
	"strings"

	"github.com/wyzzgzhdcxy/wcj-go-common/core"
)

// Run 在 dir 目录下执行 name args...，返回合并输出与执行错误（进程退出码非 0 时 err 非空）。
func Run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	core.SetHideWindow(cmd)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Git 在 dir 目录下执行 git 子命令。
func Git(dir string, args ...string) (string, error) {
	return Run(dir, "git", args...)
}
