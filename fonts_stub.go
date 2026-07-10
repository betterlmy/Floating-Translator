//go:build !windows

package main

func (a *App) GetAvailableFonts() ([]string, error) {
	return []string{"Microsoft YaHei UI"}, nil
}
