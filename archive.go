package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/git-lfs/git-lfs/tools/humanize"
	"github.com/saracen/go7z"
)

// EngineAssociationPrefix is the required engine association prefix.
const EngineAssociationPrefix = "ue4v:"

// ErrEngineAssociationNeedsPrefix is returned if the association has no
// ue4v prefix.
var ErrEngineAssociationNeedsPrefix = errors.New("engine association needs 'ue4v:' prefix")

// DownloadOptions specifies what content for the version to download.
type DownloadOptions struct {
	FetchEngine  bool
	FetchSymbols bool
}

// GetEngineAssociation returns a .uproject engine association.
//
// Path argument can be either a .uproject file or directory containing one.
func GetEngineAssociation(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	var f *os.File
	if fi.IsDir() {
		matches, _ := filepath.Glob("*.uproject")
		if len(matches) > 0 {
			f, err = os.Open(matches[0])
		}
	} else {
		f, err = os.Open(path)
	}
	if err != nil {
		return "", err
	}
	defer f.Close()

	var uproject struct {
		EngineAssociation string
	}

	err = json.NewDecoder(f).Decode(&uproject)

	return uproject.EngineAssociation, err
}

// FetchEngine fetches the engine based on engine association string.
func FetchEngine(baseURL, version string, options DownloadOptions) (string, error) {
	if !strings.HasPrefix(version, EngineAssociationPrefix) {
		return "", ErrEngineAssociationNeedsPrefix
	}

	user, err := user.Current()
	if err != nil {
		return "", err
	}

	name := strings.TrimPrefix(version, EngineAssociationPrefix)
	dest := filepath.Join(user.HomeDir, ".ue4", name)

	assetInfo := []struct {
		name    string
		enabled bool
		exists  string
		err     error
	}{
		{"engine", options.FetchEngine, "Engine/Binaries/Win64/UE4Editor.exe", nil},
		{"symbols", options.FetchSymbols, "Engine/Binaries/Win64/UE4Editor.pdb", nil},
	}

	var wg sync.WaitGroup
	for idx := range assetInfo {
		if !assetInfo[idx].enabled {
			continue
		}

		wg.Add(1)
		go func(i int, version string) {
			defer wg.Done()

			// If we have known content, skip the download
			if _, err := os.Stat(filepath.Join(dest, assetInfo[i].exists)); err == nil {
				return
			}

			assetInfo[i].err = download(baseURL, dest, assetInfo[i].name, name)
		}(idx, version)
	}
	wg.Wait()

	for idx := range assetInfo {
		if assetInfo[idx].err != nil {
			err = assetInfo[idx].err
		}
	}

	return dest, err
}

func download(baseURL, dest, asset, version string) error {
	uri, err := url.Parse(fmt.Sprintf("%s/%s-%s.7z", baseURL, asset, version))
	if err != nil {
		return err
	}

	// download
	log.Printf("Fetching %v\n", uri)
	resp, err := http.Get(uri.String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// create temp file
	file, err := ioutil.TempFile("", "ue4")
	if err != nil {
		return err
	}
	defer file.Close()

	// copy to temp file
	path := file.Name()
	_, err = io.Copy(file, io.TeeReader(resp.Body, &writeCounter{
		name:    asset,
		total:   uint64(resp.ContentLength),
		started: time.Now(),
	}))
	if err != nil {
		return err
	}

	return extract(asset, path, dest)
}

func extract(asset, path, dest string) error {
	// remove archive once extracted
	defer os.Remove(path)

	// file count
	files := func() (files int) {
		sz, err := go7z.OpenReader(path)
		if err != nil {
			return 0
		}
		defer sz.Close()

		for {
			_, err := sz.Next()
			if err != nil {
				break
			}
			files++
		}
		return
	}()

	sz, err := go7z.OpenReader(path)
	if err != nil {
		return err
	}
	defer sz.Close()

	log.Printf("Extracting %s (%d files)...\n", asset, files)
	extracted := 0
	lastUpdate := time.Now()
	for {
		hdr, err := sz.Next()
		if err == io.EOF {
			break // end of archive
		}
		if err != nil {
			return err
		}

		fpath := filepath.Join(dest, hdr.Name)
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("%s: illegal file path", fpath)
		}

		// create directory
		if hdr.IsEmptyStream && !hdr.IsEmptyFile {
			if err := os.MkdirAll(fpath, os.ModePerm); err != nil {
				return err
			}
			continue
		}

		// create file
		os.MkdirAll(filepath.Dir(fpath), os.ModePerm)
		f, err := os.Create(fpath)
		if err != nil {
			return err
		}
		defer f.Close()

		if _, err := io.Copy(f, sz); err != nil {
			return err
		}

		extracted++
		if time.Since(lastUpdate) > time.Second || extracted == files {
			log.Printf("Extracting %s (%d/%d)...\n", asset, extracted, files)
			lastUpdate = time.Now()
		}
	}

	return nil
}

type writeCounter struct {
	name     string
	total    uint64
	written  uint64
	progress int
	started  time.Time
}

func (wc *writeCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.written += uint64(n)

	if progress := int(float64(wc.written) / float64(wc.total) * 100); progress > wc.progress {
		wc.progress = progress
		log.Printf("%s - %s / %s (%d%%) %s\n", wc.name, humanize.FormatBytes(wc.written), humanize.FormatBytes(wc.total), progress, humanize.FormatByteRate(wc.written, time.Since(wc.started)))
	}
	return n, nil
}
