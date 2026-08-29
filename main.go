package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/wyzzgzhdcxy/wcj-go-common/core"
	myUtil "github.com/wyzzgzhdcxy/wcj-go-common/utils"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

// 保存全局 context，用于单例回调
var appCtx context.Context

func main() {
	processStart := time.Now()
	// InitLog 会在 {用户缓存目录}/wtools 下创建日志文件，首次运行时目录可能不存在，必须先创建
	core.MkDirALl0755(core.GetTempDir())
	myUtil.InitLog(true)

	startSt := time.Now().Format("2006-01-02 15:04:05.000")
	application := NewApp(assets)
	log.Printf("%s", startSt+" log init finish! "+time.Now().Format("2006-01-02 15:04:05.000"))

	// 后台服务是数据库唯一属主，界面数据全部经其 HTTP 接口访问，启动前先确保服务可用
	ensureSyncService()

	// 默认窗口尺寸（宽度=800，高度=屏幕高-200）
	screenWidth, screenHeight := core.GetScreenSize()
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

	// 窗口标题（包含数据目录）
	dataDir := filepath.Join(core.GetTempDir(), "data")

	// 优先从 UI 状态文件加载窗口状态，否则使用默认尺寸
	ws := application.GetWindowState()
	width := ws.Width
	height := ws.Height
	log.Printf("[DEBUG] 从 UI 状态文件加载的窗口尺寸: %dx%d", width, height)
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
	preRunElapsed := time.Since(processStart)
	title := "Git同步工具 - " + dataDir + fmt.Sprintf(" (启动耗时: %dms)", preRunElapsed.Milliseconds())
	err := wails.Run(&options.App{
		Title:             title,
		Width:             width,
		DisableResize:     false,
		Height:            height,
		Frameless:         false,
		HideWindowOnClose: false,
		StartHidden:       false,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
		BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 255},
		OnStartup: func(ctx context.Context) {
			appCtx = ctx
			application.Startup(ctx)
			// 启动完成后更新标题，显示完整启动耗时（含 WebView/前端）
			fullElapsed := time.Since(processStart)
			runtime.WindowSetTitle(ctx, fmt.Sprintf("Git同步工具 - %s (启动耗时: %dms)", dataDir, fullElapsed.Milliseconds()))
			// 如果有保存的窗口位置，应用它（坐标可能是负数，如副屏，因此不能只判断 >0）
			if ws.X != 0 || ws.Y != 0 {
				runtime.WindowSetPosition(ctx, ws.X, ws.Y)
			}
			// 如果窗口之前是最大化状态，恢复最大化
			if ws.Maximized == 1 {
				runtime.WindowMaximise(ctx)
			}
		},
		OnBeforeClose: func(ctx context.Context) bool {
			// 关闭前保存窗口状态（WebView 的 beforeunload 在窗口关闭时并不可靠）
			application.SaveCurrentWindowState()
			return false
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "wcj-go-git-singleton",
			OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
				// 新实例启动时，激活已存在的老实例窗口
				runtime.WindowUnminimise(appCtx)
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
