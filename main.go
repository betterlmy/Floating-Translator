//go:build windows || darwin

package main

import (
	"embed"
	"fmt"
	"os"

	appservice "floating-translator/internal/app"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var applicationIcon []byte

func main() {
	if err := appservice.Run(assets, applicationIcon); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
	}
}
