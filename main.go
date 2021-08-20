package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/go-ini/ini"
)

var (
	iniConfig     = flag.String("config", ".ueversionator", "ueversionator config file")
	userIniConfig = flag.String("user-config", ".uev-user", "ueversionator user config file")
	baseDir       = flag.String("basedir", "ue4", "base directory to download engine bundles in")
	bundle        = flag.String("bundle", "editor", "request UE build bundle")
	ue5           = flag.Bool("ue5", false, "UE5 build compat")
	fetchSymbols  = flag.Bool("with-symbols", false, "include UE engine debug symbols")
	virgin        = flag.Bool("virgin", false, "ask configuration options like the first time")
	assumeValid   = flag.Bool("assume-valid", false, "assumes current archive is valid, if present")
)

func handleError(err error) {
	if err == nil {
		return
	}

	fmt.Println(err)
	os.Exit(1)
}

func ueversionator() (string, string, error) {
	flag.Parse()

	// load ini config
	cfg, err := ini.LooseLoad(*iniConfig, *userIniConfig)
	handleError(err)

	baseURL, err := cfg.Section("ueversionator").GetKey("baseurl")
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

	shouldFetchSymbols := *fetchSymbols

	if !shouldFetchSymbols {
		section := "ue4v-user"
		if *ue5 {
			section = "uev-user"
		}
		symbolsConfig, err := cfg.Section(section).GetKey("symbols")
		if err == nil {
			shouldFetchSymbols = symbolsConfig.MustBool(false)
		}
	}

	dest, err := FetchEngine(downloadDir, baseURL.String(), version, DownloadOptions{
		EngineBundle: *bundle,
		FetchSymbols: shouldFetchSymbols,
		AssumeValid:  *assumeValid,
		UsesUE5:      *ue5,
	})

	return version, dest, err
}

func getDownloadDirectory(path string) string {
	cfg, _ := ini.LooseLoad(*userIniConfig)
	section := "ue4v-user"
	if *ue5 {
		section = "uev-user"
	}
	key, err := cfg.Section(section).GetKey("download_dir")
	if err != nil {
		key, _ = cfg.Section(section).NewKey("download_dir", "")
	}

	if !*virgin && key.String() != "" {
		return key.String()
	}

	fmt.Fprintf(os.Stdout, "=========================================================================\n")
	fmt.Fprintf(os.Stdout, "| A custom Unreal Engine build needs to be downloaded for this project. |\n")
	fmt.Fprintf(os.Stdout, "|    These builds can be quite large. Lots of disk space is required.   |\n")
	fmt.Fprintf(os.Stdout, "=========================================================================\n\n")
	fmt.Fprintf(os.Stdout, "Project path: %v\n\n", path)
	fmt.Fprintf(os.Stdout, "Which directory should these engine downloads be stored in?\n")

	var options []string
	const custom = "custom location"

	if user, err := user.Current(); err == nil {
		options = append(options, filepath.Join(user.HomeDir, *baseDir))
	}
	if abs, err := filepath.Abs(filepath.Join(path, "..", *baseDir)); err == nil {
		options = append(options, abs)
	}
	if runtime.GOOS == "windows" {
		options = append(options, filepath.Join(filepath.VolumeName(path), "/", *baseDir))
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
