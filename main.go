package main

import (
	"embed"
	"log"
	"path/filepath"
	"time"
	"wcj-go-common/core"
	myUtil "wcj-go-common/utils"
	"wcj-go-git/app"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	myUtil.InitLog(true)

	startSt := time.Now().Format("2006-01-02 15:04:05.000")
	core.MkDirALl0755(filepath.Join(core.GetTempDir(), "/codeGen"))
	application := app.NewApp(assets)
	log.Printf(startSt + " log init finish! " + time.Now().Format("2006-01-02 15:04:05.000"))

	// 初始化配置数据库（sqlite）
	if err := application.InitSettingsDb(); err != nil {
		log.Printf("初始化配置数据库失败: %v", err)
	}

	log.Printf("db init finish! " + time.Now().Format("2006-01-02 15:04:05.000"))

	// 默认窗口尺寸
	defaultWidth := 900
	defaultHeight := 700

	// 优先从 SQLite 加载窗口状态，否则使用默认尺寸
	ws := application.GetWindowState()
	width := ws.Width
	height := ws.Height
	if width == 0 || height == 0 {
		width = defaultWidth
		height = defaultHeight
	}

	// Create application with options
	err := wails.Run(&options.App{
		Title:         "Git同步工具",
		Width:         width,
		DisableResize: false,
		Height:        height,
		Frameless:     false,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
		BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 1},
		OnStartup:        application.Startup,
		OnShutdown:       application.Shutdown,
		Bind: []interface{}{
			application,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
	log.Printf("main start finish! " + time.Now().Format("2006-01-02 15:04:05.000"))
}
