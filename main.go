package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/go-ini/ini"
	"golang.org/x/sys/windows/registry"
)

var (
	iniConfig     = flag.String("config", ".ue4versionator", "ue4versionator config file")
	userIniConfig = flag.String("user-config", ".ue4v-user", "ue4versionator user config file")
	bundle        = flag.String("bundle", "engine", "request UE4 build bundle")
	fetchSymbols  = flag.Bool("with-symbols", false, "download and unpack UE4 engine debug symbols")
	virgin        = flag.Bool("virgin", false, "ask configuration options like the first time")
)

func main() {
	flag.Parse()

	handleError := func(err error) {
		if err == nil {
			return
		}

		fmt.Println(err)
		fmt.Print("\nPress 'Enter' to continue...")
		bufio.NewReader(os.Stdin).ReadBytes('\n')
		os.Exit(1)
	}

	// load ini config
	cfg, err := ini.LooseLoad(*iniConfig, *userIniConfig)
	handleError(err)

	baseURL, err := cfg.Section("ue4versionator").GetKey("baseurl")
	if err != nil || baseURL.String() == "" {
		handleError(fmt.Errorf("%s config file has no baseurl setting", *iniConfig))
	}

	// uproject path
	path := "."
	if len(flag.Args()) > 0 {
		path = flag.Args()[0]
	}
	path, _ = filepath.Abs(path)

	version, err := GetEngineAssociation(path)
	if err != nil {
		handleError(fmt.Errorf("error finding engine association with path %s: %v", path, err))
	}

	downloadDir, err := filepath.Abs(getDownloadDirectory(path))
	if err != nil {
		handleError(err)
	}
	handleError(os.MkdirAll(downloadDir, 0777))

	dest, err := FetchEngine(downloadDir, baseURL.String(), version, DownloadOptions{
		EngineBundle: *bundle,
		FetchSymbols: *fetchSymbols,
	})
	if err != nil {
		handleError(err)
	}

	log.Printf("Registering %s\n", version)
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Epic Games\Unreal Engine\Builds`, registry.QUERY_VALUE|registry.SET_VALUE)
	handleError(err)

	defer key.Close()

	err = key.SetStringValue(version, dest)
	handleError(err)
}

func getDownloadDirectory(path string) string {
	cfg, _ := ini.LooseLoad(*userIniConfig)
	key, err := cfg.Section("ue4v-user").GetKey("download_dir")
	if err != nil {
		key, _ = cfg.Section("ue4v-user").NewKey("download_dir", "")
	}

	if !*virgin && key.String() != "" {
		return key.String()
	}

	fmt.Fprintf(os.Stdout, "======================================================================\n")
	fmt.Fprintf(os.Stdout, "| A custom UE4 engine build needs to be downloaded for this project. |\n")
	fmt.Fprintf(os.Stdout, "|  These builds can be quite large. Lots of disk space is required.  |\n")
	fmt.Fprintf(os.Stdout, "======================================================================\n\n")
	fmt.Fprintf(os.Stdout, "Project path: %v\n\n", path)
	fmt.Fprintf(os.Stdout, "Which directory should these engine downloads be stored in?\n")

	var options []string
	const custom = "custom location"

	if user, err := user.Current(); err == nil {
		options = append(options, filepath.Join(user.HomeDir, ".ue4"))
	}
	if abs, err := filepath.Abs(filepath.Join(path, "..", ".ue4")); err == nil {
		options = append(options, abs)
	}
	if runtime.GOOS == "windows" {
		options = append(options, filepath.Join(filepath.VolumeName(path), "/", ".ue4"))
	}
	options = append(options, custom)

	var directory string
	for {
		for i, option := range options {
			fmt.Fprintf(os.Stdout, "\n%d) %s", i+1, option)
		}

		fmt.Printf("\n\nSelect an option (%d-%d) and press enter: ", 1, len(options))

		input, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		input = strings.TrimSpace(input)
		parsed, _ := strconv.ParseInt(input, 10, 8)

		chosen := int(parsed) - 1
		if chosen < 0 || chosen >= len(options) {
			fmt.Printf("Invalid '%s' option. Try again:\n\n", input)
			continue
		}

		directory = options[chosen]
		if directory == custom {
			var err error

			fmt.Printf("\nCustom location: ")
			directory, _ = bufio.NewReader(os.Stdin).ReadString('\n')

			directory, err = filepath.Abs(strings.TrimSpace(directory))
			if err != nil {
				fmt.Println(err)
				continue
			}
			if strings.HasPrefix(directory, path) {
				fmt.Println("download directory cannot reside in the project directory")
				continue
			}
			if err = os.MkdirAll(directory, 0777); err != nil {
				fmt.Println(err)
				continue
			}
		}

		if err := os.MkdirAll(directory, 0777); err != nil {
			fmt.Println(err)
			continue
		}
		break
	}

	key.SetValue(directory)
	cfg.SaveTo(*userIniConfig)

	return directory
}
