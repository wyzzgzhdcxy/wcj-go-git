// Package cmdWrapper provides a unified interface for executing shell commands
// with consistent error handling and platform-specific optimizations.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
)

// Options holds configuration for command execution
type Options struct {
	Dir        string        // Working directory
	HideWindow bool          // Hide command window on Windows
	Stdout     *bytes.Buffer // Custom stdout buffer (nil uses os.Stdout)
	Stderr     *bytes.Buffer // Custom stderr buffer (nil uses os.Stderr)
}

// defaultOptions returns default options
func defaultOptions() Options {
	return Options{
		Dir:        "",
		HideWindow: runtime.GOOS == "windows", // Hide window by default on Windows
		Stdout:     nil,
		Stderr:     nil,
	}
}

// Option is a function that modifies Options
type Option func(*Options)

// WithDir sets the working directory
func WithDir(dir string) Option {
	return func(o *Options) {
		o.Dir = dir
	}
}

// WithHideWindow sets whether to hide the command window on Windows
func WithHideWindow(hide bool) Option {
	return func(o *Options) {
		o.HideWindow = hide
	}
}

// WithStdout sets a custom stdout buffer
func WithStdout(buf *bytes.Buffer) Option {
	return func(o *Options) {
		o.Stdout = buf
	}
}

// WithStderr sets a custom stderr buffer
func WithStderr(buf *bytes.Buffer) Option {
	return func(o *Options) {
		o.Stderr = buf
	}
}

// createCommand creates an exec.Cmd with the given options
func createCommand(name string, args []string, opts Options) *exec.Cmd {
	cmd := exec.Command(name, args...)

	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}

	// Set stdout/stderr
	if opts.Stdout != nil {
		cmd.Stdout = opts.Stdout
	} else {
		cmd.Stdout = os.Stdout
	}

	if opts.Stderr != nil {
		cmd.Stderr = opts.Stderr
	} else {
		cmd.Stderr = os.Stderr
	}

	// Hide window on Windows if requested
	if runtime.GOOS == "windows" && opts.HideWindow {
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	}

	return cmd
}

// Run executes a command and returns any error
func Run(name string, args ...string) error {
	return RunWithOptions(name, args)
}

// RunWithOptions executes a command with options and returns any error
func RunWithOptions(name string, args []string, opts ...Option) error {
	options := defaultOptions()
	for _, opt := range opts {
		opt(&options)
	}

	cmd := createCommand(name, args, options)
	return cmd.Run()
}

// RunWithOutput executes a command and returns combined output
func RunWithOutput(name string, args ...string) (string, error) {
	return RunWithOutputAndOptions(name, args)
}

// RunWithOutputAndOptions executes a command with options and returns combined output
func RunWithOutputAndOptions(name string, args []string, opts ...Option) (string, error) {
	options := defaultOptions()
	for _, opt := range opts {
		opt(&options)
	}

	// For output capture, we need custom buffers
	var stdout, stderr bytes.Buffer
	options.Stdout = &stdout
	options.Stderr = &stderr

	cmd := createCommand(name, args, options)
	err := cmd.Run()

	// Combine stdout and stderr
	output := stdout.String() + stderr.String()
	return output, err
}

// Start starts a command but does not wait for it to complete
func Start(name string, args ...string) error {
	return StartWithOptions(name, args)
}

// StartWithOptions starts a command with options but does not wait for it to complete
func StartWithOptions(name string, args []string, opts ...Option) error {
	options := defaultOptions()
	for _, opt := range opts {
		opt(&options)
	}

	cmd := createCommand(name, args, options)
	return cmd.Start()
}

// RunWithDir is a convenience function to run a command in a specific directory
func RunWithDir(dir, name string, args ...string) error {
	return RunWithOptions(name, args, WithDir(dir))
}

// RunWithDirAndOutput is a convenience function to run a command in a specific directory and get output
func RunWithDirAndOutput(dir, name string, args ...string) (string, error) {
	return RunWithOutputAndOptions(name, args, WithDir(dir))
}

func GetRsaPrivateKeyPath() string {
	userDir, _ := os.UserHomeDir()
	return userDir + "\\.ssh\\id_rsa"
}

// GitClone 克隆仓库（使用命令行 git）
func GitClone(repoURL, targetDir string) error {
	cmd := exec.Command("git", "clone", repoURL, targetDir)
	cmd.Env = append(os.Environ(), "GIT_SSH_COMMAND=ssh -i "+GetRsaPrivateKeyPath())
	return cmd.Run()
}

// GitPush 推送（使用命令行 git）
func GitPush(repoPath string) error {
	cmd := exec.Command("git", "push")
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(), "GIT_SSH_COMMAND=ssh -i "+GetRsaPrivateKeyPath())
	return cmd.Run()
}

// GitPull 拉取（使用命令行 git）
func GitPull(repoPath string) string {
	cmd := exec.Command("git", "pull")
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(), "GIT_SSH_COMMAND=ssh -i "+GetRsaPrivateKeyPath())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(output))
	}
	return ""
}

// GitAdd 添加文件（使用命令行 git）
func GitAdd(repoPath string) error {
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = repoPath
	return cmd.Run()
}

// AddCommitPush 提交并推送（使用命令行 git）
func AddCommitPush(repoPath string) string {
	// git add .
	if err := GitAdd(repoPath); err != nil {
		return fmt.Sprintf("添加文件失败: %v", err)
	}

	// git commit -m "wtools update"
	cmd := exec.Command("git", "commit", "-m", "wtools update")
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(output))
	}

	// git push
	cmd = exec.Command("git", "push")
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(), "GIT_SSH_COMMAND=ssh -i "+GetRsaPrivateKeyPath())
	output, err = cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(output))
	}

	return ""
}

// PullAddCommitPush 拉取后提交推送
func PullAddCommitPush(repoPath string) string {
	if len(GitPull(repoPath)) == 0 {
		return AddCommitPush(repoPath)
	}
	return "拉取仓库失败, repoPath:" + repoPath
}

// OpenUrl 打开URL（跨平台实现）
func OpenUrl(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}

	return exec.Command(cmd, args...).Start()
}
