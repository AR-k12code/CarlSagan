package main

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/9072997/jgh"
	"github.com/natefinch/atomic"
)

func getCacheDir() string {
	// get the directory of this executable
	exePath, err := os.Executable()
	jgh.PanicOnErr(err)
	exeFolder := filepath.Dir(exePath)

	// "cache" folder is next to this executable
	cacheDir := filepath.Join(exeFolder, "cache")

	// attempt to create the directory
	// if this fails with "ErrExist", ignore it
	err = os.Mkdir(cacheDir, 0700)
	if err != nil && !errors.Is(err, os.ErrExist) {
		panic(err)
	}

	return cacheDir
}

func pathHash(path []string) string {
	pathString := pathToString(path)
	return jgh.MD5(pathString)
}

// deletes files older than config.MaxAge from the cache
func cleanCache() {
	cacheDir := getCacheDir()
	cacheItems, err := ioutil.ReadDir(cacheDir)
	jgh.PanicOnErr(err)

	config.mutex.Lock()
	maxAge := time.Duration(config.MaxAge) * time.Second
	config.mutex.Unlock()

	for _, file := range cacheItems {
		// if file is too old
		if time.Now().Sub(file.ModTime()) > maxAge {
			// delete the file (ignore errors)
			os.Remove(filepath.Join(cacheDir, file.Name()))
		}
	}
	return
}

func addToCache(path []string, data string) {
	hash := pathHash(path)
	file := filepath.Join(getCacheDir(), hash)
	// atomically write data to file
	err := atomic.WriteFile(file, strings.NewReader(data))
	jgh.PanicOnErr(err)
}

// get an item form the cache. Aditionally report it's age in seconds or -1
// if the item was not in the cache
func getFromCache(path []string) (data string, age int) {
	hash := pathHash(path)
	file := filepath.Join(getCacheDir(), hash)

	// get file modified time
	fileInfo, err := os.Stat(file)
	// if we get a "file does not exist" error, report that
	// the item is not in the cache
	if errors.Is(err, os.ErrNotExist) {
		return "", -1
	}
	age = int(time.Now().Sub(fileInfo.ModTime()) / time.Second)

	// try to read the file
	dataBytes, err := ioutil.ReadFile(file)
	// if we get a "file does not exist" error, report that the item is not
	// in the cache. This is unlikely (we already checked), but could happen
	// if the file is deleted between the first check and the file read.
	if errors.Is(err, os.ErrNotExist) {
		return "", -1
	}
	jgh.PanicOnErr(err)

	return string(dataBytes), age
}
