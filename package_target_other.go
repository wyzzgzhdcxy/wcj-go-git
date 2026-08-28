//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

func getPackageTargetDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "我的工具箱")
}