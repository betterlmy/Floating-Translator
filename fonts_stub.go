//go:build !windows && !darwin

package main

func (a *App) GetAvailableFonts() ([]string, error) {
	return []string{"Microsoft YaHei UI"}, nil
}
