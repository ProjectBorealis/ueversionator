package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
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
func FetchEngine(rootDir string, baseURL, version string, options DownloadOptions) (string, error) {
	if !strings.HasPrefix(version, EngineAssociationPrefix) {
		return "", ErrEngineAssociationNeedsPrefix
	}

	name := strings.TrimPrefix(version, EngineAssociationPrefix)
	dest := filepath.Join(rootDir, name)

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

	var err error
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

	req, _ := http.NewRequest("GET", uri.String(), nil)

	// if archive exists, see if we can do a range request
	archivePath := dest + ".7z"
	if fi, err := os.Stat(archivePath); err == nil {
		resp, err := http.Head(uri.String())
		if err != nil {
			return err
		}
		if resp.Header.Get("Accept-Ranges") == "bytes" {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", fi.Size()))
		}
	}

	var file *os.File
	var body io.ReadCloser
	var size int64
	for i := 0; i < 2; i++ {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		body = resp.Body
		size = resp.ContentLength

		switch resp.StatusCode {
		case http.StatusOK:
			log.Printf("Fetching %v\n", uri)
			file, err = os.Create(archivePath)

		case http.StatusPartialContent:
			log.Printf("Resuming %v\n", uri)
			file, err = os.OpenFile(archivePath, os.O_WRONLY|os.O_APPEND, 0644)

		case http.StatusRequestedRangeNotSatisfiable:
			req.Header.Del("Range")
			continue

		}
		if err != nil {
			return err
		}
		break
	}
	defer body.Close()

	// copy to temp file
	path := file.Name()
	_, err = io.Copy(file, io.TeeReader(body, &writeCounter{
		name:    asset,
		total:   uint64(size),
		started: time.Now(),
	}))
	if err != nil {
		return err
	}

	return extract(asset, path, dest)
}

func extract(asset, path, dest string) (err error) {
	// remove archive once extracted
	defer func() {
		if err == nil {
			err = os.Remove(path)
		}
	}()

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
			extracted++
			continue
		}

		// create file
		os.MkdirAll(filepath.Dir(fpath), os.ModePerm)
		f, err := os.Create(fpath)
		if err != nil {
			return err
		}
		if _, err = io.Copy(f, sz); err != nil {
			f.Close()
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		os.Chtimes(fpath, hdr.AccessedAt, hdr.ModifiedAt)

		extracted++
		if time.Since(lastUpdate) > time.Second || extracted == files {
			log.Printf("Extracting %s (%d/%d)...\n", asset, extracted, files)
			lastUpdate = time.Now()
		}
	}

	if extracted != files {
		return fmt.Errorf("error: expected to extract %d items, only extracted %d", files, extracted)
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
