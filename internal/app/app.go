package app

import (
	"context"
	"sync"
	"sync/atomic"

	"floating-translator/internal/config"
	"floating-translator/internal/logger"
	"floating-translator/internal/platform"
	"floating-translator/internal/processor"
	subtitlepkg "floating-translator/internal/subtitle"
)

const (
	settingsRefreshEvent  = "settings:refresh"
	settingsWindowWidth   = 1080
	settingsWindowHeight  = 760
	subtitleHeightPercent = 28
)

type workArea struct {
	X      int
	Y      int
	Width  int
	Height int
}

type windowBounds = subtitlepkg.Bounds

type applicationController interface {
	Quit()
	PrimaryWorkArea() (workArea, error)
}

type windowController interface {
	Show()
	Hide()
	Focus()
	SetAlwaysOnTop(enabled bool)
	SetSize(width int, height int)
	SetPosition(x int, y int)
	EmitEvent(name string, data any)
}

type subtitleController = subtitlepkg.Controller

// App 管理应用配置、翻译调度和桌面生命周期。
type App struct {
	mutex              sync.RWMutex
	configurationMutex sync.Mutex

	context        context.Context
	application    applicationController
	subtitle       subtitleController
	settingsWindow windowController
	logger         *logger.Logger
	desktop        platform.Desktop
	processor      *processor.Processor
	paths          config.Paths
	config         config.Config

	preparePaths func() (config.Paths, bool, error)

	configValid        bool
	frontendReady      bool
	listening          bool
	settingsOpen       bool
	selectionReadMutex sync.Mutex
	initialized        atomic.Bool
}

func (a *App) setWindows(app applicationController, subtitle subtitleController, settingsWindow windowController) {
	a.application = app
	a.subtitle = subtitle
	a.settingsWindow = settingsWindow
}

// NewApp 创建应用实例。
func NewApp() *App {
	return NewAppWithIcon(nil)
}

func NewAppWithIcon(icon []byte) *App {
	desktop := platform.NewDesktop()
	desktop.SetApplicationIcon(icon)
	return &App{
		logger:       logger.NewNop(),
		desktop:      desktop,
		preparePaths: config.PreparePaths,
	}
}
