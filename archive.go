package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/vbauerster/mpb/v7/cwriter"
	"github.com/vbauerster/mpb/v7/decor"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gen2brain/go-unarr"
	"github.com/vbauerster/mpb/v7"
)

// EngineAssociationPrefix is the required engine association prefix.
const EngineAssociationPrefix = "uev:"

// ErrEngineAssociationNeedsPrefix is returned if the association has no
// uev prefix.
var ErrEngineAssociationNeedsPrefix = errors.New("engine association needs 'uev:' prefix")

// DownloadOptions specifies what content for the version to download.
type DownloadOptions struct {
	EngineBundle string
	FetchSymbols bool
	AssumeValid  bool
	UsesUE5      bool
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

// GetBundleVerificationFile returns the file that should exist for this bundle as a basic integrity check
//
// If the bundle contains "engine", then it is considered an engine bundle, and thus must include Unreal Game.
// Else, it is considered an editor bundle, and must include Unreal Editor.
func GetBundleVerificationFile(bundle string, usesUE5 bool) string {
	if strings.Contains(bundle, "engine") {
		if usesUE5 {
			return "Engine/Binaries/Win64/UnrealGame."
		}
		return "Engine/Binaries/Win64/UE4Game."
	} else {
		if usesUE5 {
			return "Engine/Binaries/Win64/UnrealEditor."
		}
		return "Engine/Binaries/Win64/UE4Editor."
	}
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
		{options.EngineBundle, true, GetBundleVerificationFile(options.EngineBundle, options.UsesUE5) + "exe", nil},
		{options.EngineBundle + "-symbols", options.FetchSymbols, GetBundleVerificationFile("editor", options.UsesUE5) + "pdb", nil},
	}

	var wg sync.WaitGroup
	duration := time.Millisecond * 120
	p := mpb.New(mpb.WithWaitGroup(&wg), mpb.WithRefreshRate(duration))
	if !cwriter.IsTerminal(int(os.Stdout.Fd())) {
		p = mpb.New(
			mpb.WithWaitGroup(&wg),
			mpb.ContainerOptional(
				// setting to nil will:
				// set output to ioutil.Discard and disable refresh rate cycle, in
				// order not to consume much CPU. Hovewer a single refresh still will
				// be triggered on bar complete event, per each bar.
				mpb.WithOutput(nil),
				true,
			),
		)
	}

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

			assetInfo[i].err = download(p, baseURL, rootDir, name, assetInfo[i].name, name, options.AssumeValid)
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

func download(p *mpb.Progress, baseURL, rootDir, name, asset, version string, assumeValid bool) error {
	urlStr := fmt.Sprintf("%s/%s-%s.7z", baseURL, asset, version)
	uri, err := url.Parse(urlStr)
	if err != nil {
		return err
	}

	req, _ := http.NewRequest("GET", uri.String(), nil)
	dest := filepath.Join(rootDir, name)

	// if archive exists, see if we can do a range request
	archivePath := filepath.Join(rootDir, asset) + "-" + name + ".7z"
	if fi, err := os.Stat(archivePath); err == nil {
		if assumeValid {
			return extract(p, asset, archivePath, dest)
		} else {
			resp, err := http.Head(uri.String())
			if err != nil {
				return err
			}
			if resp.StatusCode >= 400 {
				return errors.New(fmt.Sprintf("%s: %s", resp.Status, urlStr))
			}
			if resp.Header.Get("Content-Length") != "" {
				size, err := strconv.Atoi(resp.Header.Get("Content-Length"))
				if err == nil && int64(size) == fi.Size() {
					return extract(p, asset, archivePath, dest)
				}
			}
			if resp.Header.Get("Accept-Ranges") == "bytes" {
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-", fi.Size()))
			}
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
		if resp.StatusCode >= 400 {
			return errors.New(fmt.Sprintf("%s: %s", resp.Status, urlStr))
		}
		break
	}
	defer body.Close()

	// copy to temp file
	_, err = io.Copy(file, io.TeeReader(body, &writeCounter{
		name:    asset,
		total:   uint64(size),
		started: time.Now(),
	}))
	if err != nil {
		return err
	}

	return extract(p, asset, archivePath, dest)
}

func extract(p *mpb.Progress, asset, path, dest string) (err error) {
	// remove archive once extracted
	defer func() {
		if err == nil {
			err = os.Remove(path)
		}
	}()

	// file count
	files := func() (files int64) {
		a, err := unarr.NewArchive(path)
		if err != nil {
			return 0
		}
		defer a.Close()

		list, err := a.List()
		if err != nil {
			return 0
		}
		return int64(len(list))
	}()

	count := files

	fmt.Printf("Extracting %s\n", path)

	a, err := unarr.NewArchive(path)
	if err != nil {
		return err
	}
	defer a.Close()

	bar := p.AddBar(count,
		mpb.PrependDecorators(
			decor.Name(asset, decor.WC{W: len(asset) + 1, C: decor.DidentRight}),
			decor.CountersNoUnit("[%d / %d files]", decor.WCSyncWidth),
		),
		mpb.AppendDecorators(
			decor.OnComplete(
				decor.Percentage(),
				"Done!",
			),
			decor.EwmaSpeed(0, "% .0f files/s", 60),
			decor.Name(" ETA "),
			decor.EwmaETA(decor.ET_STYLE_GO, 90),
		),
	)

	start := time.Now()

	for {
		err := a.Entry()
		if err != nil {
			if err == io.EOF {
				break
			}

			return err
		}

		fpath := filepath.Join(dest, a.Name())
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("%s: illegal file path", fpath)
		}

		// if sizes don't match, or it doesn't exist, write it out
		if stat, err := os.Stat(fpath); os.IsNotExist(err) || int(stat.Size()) != a.Size() {
			os.MkdirAll(filepath.Dir(fpath), os.ModePerm)

			data, err := a.ReadAll()
			if err != nil {
				return err
			}

			err = ioutil.WriteFile(fpath, data, os.ModePerm)
			if err != nil {
				return err
			}

			modTime := a.ModTime()
			os.Chtimes(fpath, modTime, modTime)
		}

		bar.Increment()
		bar.DecoratorEwmaUpdate(time.Since(start))
		start = time.Now()
	}

	extracted := bar.Current()
	if extracted != count {
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
		log.Printf("%s - %s / %s (%d%%) %s\n", wc.name, formatBytes(wc.written), formatBytes(wc.total), progress, formatByteRate(wc.written, time.Since(wc.started)))
	}
	return n, nil
}

// Below code (formatBytes and formatByteRate) is based on saracen/lfscache
// Copyright (c) 2018 Arran Walker
//
// Permission is hereby granted, free of charge, to any person obtaining
// a copy of this software and associated documentation files (the "Software"),
// to deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.

const (
	unit     = 1000
	prefixes = "KMGTPE"
)

func formatBytes(b uint64) string {
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}

	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), prefixes[exp])
}

func formatByteRate(s uint64, d time.Duration) string {
	b := uint64(float64(s) / math.Max(time.Nanosecond.Seconds(), d.Seconds()))
	if b < unit {
		return fmt.Sprintf("%dB/s", b)
	}

	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB/s", float64(b)/float64(div), prefixes[exp])
}
