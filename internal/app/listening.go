package app

import (
	"floating-translator/internal/logger"
	"floating-translator/internal/platform"
)

func (a *App) toggleListening() {
	a.mutex.Lock()
	if !a.configValid || !a.frontendReady {
		a.mutex.Unlock()
		return
	}
	a.listening = !a.listening
	listening := a.listening
	a.mutex.Unlock()
	a.setListening(listening)
	a.logger.Info("剪切板监听状态已更新", logger.Bool("enabled", listening))
}

func (a *App) setListening(enabled bool) {
	a.mutex.Lock()
	a.listening = enabled
	a.mutex.Unlock()
	a.processor.SetEnabled(enabled)
	a.desktop.SetListening(enabled)
	if enabled {
		a.desktop.SetTrayStatus(platform.TrayStatusRunning)
	} else {
		a.desktop.SetTrayStatus(platform.TrayStatusPaused)
	}
}
