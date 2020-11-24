package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/9072997/jgh"
	_ "github.com/mattn/go-sqlite3"
	"github.com/mitchellh/hashstructure"
	"github.com/natefinch/atomic"
)

type reportIdentifier struct {
	Path          []string
	PromptAnswers map[string]string
}

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

func getUsageFile() string {
	// get the directory of this executable
	exePath, err := os.Executable()
	jgh.PanicOnErr(err)
	exeFolder := filepath.Dir(exePath)

	// "usage.sqlite3" file is next to this executable
	usageFile := filepath.Join(exeFolder, "usage.sqlite3")

	// use classic path without prefix (sqlite needs this)
	usageFile = strings.TrimPrefix(usageFile, `\\?\`)

	return usageFile
}

func recordUse(path []string, promptAnswers map[string]string) {
	usageFile := getUsageFile()
	hash := pathHash(path, promptAnswers)
	pathStr := pathToString(path)
	answersJSON, err := json.Marshal(promptAnswers)
	jgh.PanicOnErr(err)
	lastUsed := time.Now().Unix()

	// try 3 times in case we get "file in use"
	success, msg := jgh.Try(1, 3, false, "", func() bool {
		// open database
		db, err := sql.Open("sqlite3", usageFile)
		jgh.PanicOnErr(err)
		defer db.Close()

		// create table if it does not exist
		query, err := db.Prepare(`
			CREATE TABLE IF NOT EXISTS usage (
				hash TEXT PRIMARY KEY,
				path TEXT NOT NULL,
				promptAnswers TEXT NULL,
				lastUsed INTEGER NOT NULL
			)
		`)
		jgh.PanicOnErr(err)
		_, err = query.Exec()
		jgh.PanicOnErr(err)

		// set last Used time for given hash to now
		query, err = db.Prepare(`
			INSERT OR REPLACE INTO usage
				(hash, path, promptAnswers, lastUsed)
			VALUES
				(?, ?, ?, ?)
		`)
		jgh.PanicOnErr(err)
		_, err = query.Exec(hash, pathStr, answersJSON, lastUsed)
		jgh.PanicOnErr(err)
		return true
	})

	if !success && !runningAsCGI {
		log.Println(msg)
	}
}

func warmCache(usedWithin uint) {
	usageFile := getUsageFile()
	// get the mimimum "last used" value for an item to be warmed
	minTimestamp := time.Now().Unix() - int64(usedWithin)

	var reportsToWarm []reportIdentifier
	// try 3 times in case we get "file in use"
	jgh.Try(1, 3, true, "", func() bool {
		// open database
		db, err := sql.Open("sqlite3", usageFile)
		jgh.PanicOnErr(err)
		defer db.Close()

		// create table if it does not exist
		query, err := db.Prepare(`
			CREATE TABLE IF NOT EXISTS usage (
				hash TEXT PRIMARY KEY,
				path TEXT NOT NULL,
				promptAnswers TEXT NULL,
				lastUsed INTEGER NOT NULL
			)
		`)
		jgh.PanicOnErr(err)
		_, err = query.Exec()
		jgh.PanicOnErr(err)

		// get recently used items
		query, err = db.Prepare(
			"SELECT path, promptAnswers FROM usage WHERE lastUsed >= ?",
		)
		jgh.PanicOnErr(err)
		rows, err := query.Query(minTimestamp)
		jgh.PanicOnErr(err)
		defer rows.Close()
		for rows.Next() {
			var pathStr, answersJSON string
			err := rows.Scan(&pathStr, &answersJSON)
			jgh.PanicOnErr(err)
			// de-serialize path and prompt answers
			path := ParsePath(pathStr)
			var promptAnswers map[string]string
			err = json.Unmarshal([]byte(answersJSON), &promptAnswers)
			jgh.PanicOnErr(err)

			reportsToWarm = append(reportsToWarm, reportIdentifier{
				Path:          path,
				PromptAnswers: promptAnswers,
			})
		}

		return true
	})

	// warm each path
	for _, report := range reportsToWarm {
		if !runningAsCGI {
			log.Println(report)
		}
		// ignore errors
		success, msg := jgh.Try(0, 1, false, "", func() bool {
			// we warm the cache by just running through the normal steps to
			// prepare a response, but we specify that the data must be new
			PrepareResponse(false, report.Path, report.PromptAnswers, 0)
			return true
		})
		if !success && !runningAsCGI {
			log.Println(msg)
		}
	}
}

// BUG(jon): What are the implications of this not being a trusted one-way
// function?
func pathHash(path []string, promptAnswers map[string]string) string {
	hash, err := hashstructure.Hash(reportIdentifier{
		Path:          path,
		PromptAnswers: promptAnswers,
	}, &hashstructure.HashOptions{ZeroNil: true})
	jgh.PanicOnErr(err)
	return fmt.Sprintf("%016X", hash)
}

// deletes files older than config.MaxAge from the cache
func cleanCache() {
	cacheDir := getCacheDir()
	cacheItems, err := ioutil.ReadDir(cacheDir)
	jgh.PanicOnErr(err)

	config.mutex.Lock()
	maxAge := time.Duration(config.MaxAge) * time.Second
	config.mutex.Unlock()

	// BUG(jon): there is a race condition here. We could identify an old
	// item, the item could be updated, then we could delete it. This seems
	// unlikely and the only thing that happens is an unnecessary cache miss
	// next time, so I'm not going to fix it.
	for _, file := range cacheItems {
		// if file is too old
		if time.Now().Sub(file.ModTime()) > maxAge {
			// delete the file (ignore errors)
			os.Remove(filepath.Join(cacheDir, file.Name()))
		}
	}
	return
}

func addToCache(hash string, data string) {
	file := filepath.Join(getCacheDir(), hash)
	// atomically write data to file
	err := atomic.WriteFile(file, strings.NewReader(data))
	jgh.PanicOnErr(err)
}

// get an item form the cache. Aditionally report it's age in seconds or -1
// if the item was not in the cache
func getFromCache(hash string) (data string, age int) {
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
