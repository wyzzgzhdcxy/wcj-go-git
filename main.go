package main

import (
	"context"
	"embed"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"wcj-go-common/core"
	myUtil "wcj-go-common/utils"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

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

//go:embed all:frontend/dist
var assets embed.FS

// 保存全局 context，用于单例回调
var appCtx context.Context

// 检查是否应该隐藏启动
func isHiddenStart() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--hidden" || arg == "-h" {
			return true
		}
	}
	return false
}

func main() {
	myUtil.InitLog(true)

	startSt := time.Now().Format("2006-01-02 15:04:05.000")
	core.MkDirALl0755(filepath.Join(core.GetTempDir(), "/codeGen"))
	application := NewApp(assets)
	log.Printf("%s", startSt+" log init finish! "+time.Now().Format("2006-01-02 15:04:05.000"))

	// 初始化配置数据库（sqlite）
	if err := application.InitSettingsDb(); err != nil {
		log.Printf("初始化配置数据库失败: %v", err)
	}

	log.Printf("%s", "db init finish! "+time.Now().Format("2006-01-02 15:04:05.000"))

	// 默认窗口尺寸（宽度=800，高度=屏幕高-200）
	screenWidth, screenHeight := getScreenSize()
	defaultWidth := 820
	defaultHeight := 700
	// 最大尺寸限制
	maxWidth := screenWidth - 100
	maxHeight := screenHeight - 50
	if defaultWidth > maxWidth {
		defaultWidth = maxWidth
	}
	if defaultHeight > maxHeight {
		defaultHeight = maxHeight
	}
	log.Printf("屏幕尺寸: %dx%d, 默认窗口: %dx%d", screenWidth, screenHeight, defaultWidth, defaultHeight)

	// 窗口标题（包含数据库路径）
	dbPath := core.GetTempDir() + "/data/sync_list.db"

	// 优先从 SQLite 加载窗口状态，否则使用默认尺寸
	ws := application.GetWindowState()
	width := ws.Width
	height := ws.Height
	log.Printf("[DEBUG] 从数据库加载的窗口尺寸: %dx%d", width, height)
	if width == 0 || height == 0 {
		width = defaultWidth
		height = defaultHeight
		log.Printf("[DEBUG] 使用默认尺寸: %dx%d", width, height)
	}
	// 确保窗口尺寸不会超过屏幕
	if width > maxWidth {
		width = maxWidth
		log.Printf("[DEBUG] 宽度超过限制，调整为: %dx%d", width, height)
	}
	if height > maxHeight {
		height = maxHeight
		log.Printf("[DEBUG] 高度超过限制，调整为: %dx%d", width, height)
	}
	log.Printf("[DEBUG] 最终使用的窗口尺寸: %dx%d, 位置: %d,%d", width, height, ws.X, ws.Y)

	// Create application with options
	err := wails.Run(&options.App{
		Title:             "Git同步工具 - " + dbPath,
		Width:             width,
		DisableResize:     false,
		Height:            height,
		Frameless:         false,
		HideWindowOnClose: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
		BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 1},
		OnStartup: func(ctx context.Context) {
			appCtx = ctx
			application.Startup(ctx)
			// 如果有保存的窗口位置，应用它
			if ws.X > 0 || ws.Y > 0 {
				runtime.WindowSetPosition(ctx, ws.X, ws.Y)
			}
			// 如果窗口之前是最大化状态，恢复最大化
			if ws.Maximized == 1 {
				runtime.WindowMaximise(ctx)
			}
			// 如果带 --hidden 或 -h 参数启动，则隐藏窗口
			if isHiddenStart() {
				runtime.WindowHide(ctx)
			}
		},
		OnShutdown: application.Shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "wcj-go-git-singleton",
			OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
				// 新实例启动时，激活已存在的老实例窗口
				runtime.Show(appCtx)
			},
		},
		Bind: []interface{}{
			application,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
	log.Printf("%s", "main start finish! "+time.Now().Format("2006-01-02 15:04:05.000"))
}
