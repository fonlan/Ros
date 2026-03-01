//go:build windows

package main

import (
	"log"
	"os"
)

func main() {
	app, err := NewRosApp()
	if err != nil {
		log.Printf("failed to initialize app: %v", err)
		os.Exit(1)
	}

	if err := app.Run(); err != nil {
		log.Printf("application error: %v", err)
		os.Exit(1)
	}
}
