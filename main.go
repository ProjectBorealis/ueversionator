package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/go-ini/ini"
	"golang.org/x/sys/windows/registry"
)

var (
	iniConfig    = flag.String("config", ".ue4versionator", "ue4versionator config file")
	fetchEngine  = flag.Bool("with-engine", true, "download and unpack UE4 engine build")
	fetchSymbols = flag.Bool("with-symbols", false, "download and unpack UE4 engine debug symbols")
)

func main() {
	flag.Parse()

	handleError := func(err error) {
		fmt.Println(err)
		fmt.Print("\nPress 'Enter' to continue...")
		bufio.NewReader(os.Stdin).ReadBytes('\n')
		os.Exit(1)
	}

	// load ini config
	cfg, err := ini.Load(*iniConfig)
	if err != nil {
		handleError(fmt.Errorf("%s config file not found", *iniConfig))
	}
	baseURL, err := cfg.Section("ue4versionator").GetKey("baseurl")
	if err != nil || baseURL.String() == "" {
		handleError(fmt.Errorf("%s config file has no baseurl setting", *iniConfig))
	}

	// uproject path
	path := "."
	if len(flag.Args()) > 0 {
		path = flag.Args()[0]
	}

	version, err := GetEngineAssociation(path)
	if err != nil {
		handleError(fmt.Errorf("error finding engine association with path %s: %v", path, err))
	}

	dest, err := FetchEngine(baseURL.String(), version, DownloadOptions{
		FetchEngine:  *fetchEngine,
		FetchSymbols: *fetchSymbols,
	})
	if err != nil {
		handleError(err)
	}

	log.Printf("Registering %s\n", version)
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Epic Games\Unreal Engine\Builds`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		handleError(err)
	}
	defer key.Close()

	if err = key.SetStringValue(version, dest); err != nil {
		handleError(err)
	}
}
