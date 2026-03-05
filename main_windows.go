//go:build windows

package main

import (
	"errors"
	"log"
	"os"
)

func main() {
	instanceMutex, err := acquireSingleInstanceMutex()
	if err != nil {
		if errors.Is(err, errSingleInstanceAlreadyRunning) {
			showAlreadyRunningMessage()
			os.Exit(0)
		}
		log.Printf("failed to enable single-instance mode: %v", err)
		os.Exit(1)
	}
	defer releaseSingleInstanceMutex(instanceMutex)

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
