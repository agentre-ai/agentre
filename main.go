package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"log"
	"os"
	stdruntime "runtime"
	"strings"

	"agentre/internal/app"
	"agentre/internal/bootstrap"
	"agentre/internal/cli/claudecodecmd"

	"github.com/cago-frame/cago/pkg/logger"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"go.uber.org/zap"
)

//go:embed all:frontend/dist
var assets embed.FS

const (
	defaultWindowWidth  = 1024
	defaultWindowHeight = 768
	minWindowWidth      = 860
	minWindowHeight     = 640
)

func main() {
	// CLI mode: when invoked as `agentre claudecode …` (e.g. by the claude
	// code hook child process), short-circuit before booting wails/cago.
	if len(os.Args) >= 2 && os.Args[1] == "claudecode" {
		claudecodecmd.Main(os.Args[2:])
		return
	}

	runtime, err := bootstrap.Init(context.Background())
	if err != nil {
		log.Fatalf("init cago: %v", err)
	}
	defer runtime.Close()

	// Create an instance of the app structure
	appInst := app.NewApp()

	// Create application with options
	err = wails.Run(newWailsOptionsForDataDir(appInst, assets, stdruntime.GOOS, runtime.DataDir()))

	if err != nil {
		logger.Default().Error("wails run failed", zap.Error(err))
		log.Fatalf("wails run failed: %v", err)
	}
}

func newWailsOptionsForDataDir(a *app.App, assets fs.FS, goos, dataDir string) *options.App {
	appOptions := &options.App{
		Title:       "Agentre",
		Width:       defaultWindowWidth,
		Height:      defaultWindowHeight,
		MinWidth:    minWindowWidth,
		MinHeight:   minWindowHeight,
		StartHidden: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        a.Startup,
		OnShutdown:       a.Shutdown,
		OnBeforeClose:    a.OnBeforeClose,
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
		Bind: []interface{}{
			a,
		},
	}

	if !isWailsDevMode() {
		appOptions.SingleInstanceLock = &options.SingleInstanceLock{
			UniqueId: singleInstanceUniqueID(dataDir),
			OnSecondInstanceLaunch: func(secondInstanceData options.SecondInstanceData) {
				logger.Default().Info("second instance launch",
					zap.Strings("args", secondInstanceData.Args),
					zap.String("workingDirectory", secondInstanceData.WorkingDirectory))
			},
		}
	}

	configurePlatformWindowOptions(appOptions, goos)

	return appOptions
}

func isWailsDevMode() bool {
	return strings.TrimSpace(os.Getenv("devserver")) != ""
}

func singleInstanceUniqueID(dataDir string) string {
	sum := sha256.Sum256([]byte(dataDir))
	return "agentre-" + hex.EncodeToString(sum[:8])
}

func configurePlatformWindowOptions(appOptions *options.App, goos string) {
	if goos != "windows" {
		return
	}

	appOptions.Frameless = true
	appOptions.Windows = &windows.Options{
		Theme: windows.SystemDefault,
		// Keep native shadow and rounded corners while the titlebar itself is custom.
		DisableFramelessWindowDecorations: false,
	}
}
