//go:build windows

package main

import (
	"os"
	"path/filepath"
)

func getPackageTargetDir() string {
	dir := `E:\application\我的工具箱`
	// 如果 E 盘不存在，则回退到用户主目录
	if _, err := os.Stat(`E:\`); err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "我的工具箱")
	}
	return dir
}