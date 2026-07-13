//go:build bindings

package app

import "github.com/wailsapp/wails/v3/pkg/application"

// bindingService 仅供 Wails 绑定生成器发现 App 服务。
func bindingService() application.Service {
	return application.NewService(NewApp())
}
