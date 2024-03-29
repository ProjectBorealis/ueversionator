package main

import (
	"golang.org/x/sys/windows/registry"
	"log"
)

func main() {
	version, dest, err := ueversionator()
	handleError(err)

	log.Printf("Registering %s\n", version)
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Epic Games\Unreal Engine\Builds`, registry.QUERY_VALUE|registry.SET_VALUE)
	handleError(err)

	defer key.Close()

	err = key.SetStringValue(version, dest)
	handleError(err)
}
