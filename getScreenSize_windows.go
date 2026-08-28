//go:build windows

package main

import "syscall"

// 获取屏幕尺寸（Windows API，考虑DPI缩放）
func getScreenSize() (width, height int) {
	user32 := syscall.NewLazyDLL("user32.dll")
	// 获取 DPI 缩放因子
	// GetDpiForSystem 在 user32.dll 中
	procDPI := user32.NewProc("GetDpiForSystem")
	dpi, _, _ := procDPI.Call()
	scale := float64(dpi) / 96.0 // 96 DPI 是默认的

	// 获取屏幕物理像素
	proc := user32.NewProc("GetSystemMetrics")
	r1, _, _ := proc.Call(uintptr(0))
	r2, _, _ := proc.Call(uintptr(1))
	physWidth := int(r1)
	physHeight := int(r2)

	// 转换为逻辑像素（考虑缩放）
	width = int(float64(physWidth) / scale)
	height = int(float64(physHeight) / scale)

	// 防止返回0导致问题
	if width <= 0 {
		width = 1920
	}
	if height <= 0 {
		height = 1080
	}
	return
}