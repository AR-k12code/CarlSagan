package main

import (
	"encoding/json"
	"io/ioutil"
	"strings"
	"sync"

	"./cognos"

	"github.com/9072997/jgh"
)

var config struct {
	CognosUsername  string            `json:"cognosUsername"`
	CognosPassword  string            `json:"cognosPassword"`
	ReportPasswords map[string]string `json:"reports"`
	MasterPassword  string            `json:"masterPassword"`
	mutex           sync.Mutex
}

// Lock the mutex before calling
func readConfig(filename string) {
	configJSON, err := ioutil.ReadFile(filename)
	jgh.PanicOnErr(err)
	json.Unmarshal(configJSON, config) //nolint:go-vet
}

// Lock the mutex before calling
func writeConfig(filename string) {
	configJSON, err := json.Marshal(config) //nolint:go-vet
	jgh.PanicOnErr(err)
	ioutil.WriteFile(filename, configJSON, 0600)
}

func pathToString(path []string) string {
	// check that no path components contain a slash
	if strings.Contains(strings.Join(path, ""), "/") {
		panic("A path component may not contain a slash")
	}

	return strings.Join(path, "/")
}

func reportPassword(path []string) string {
	config.mutex.Lock()
	defer config.mutex.Unlock()

	pathString := pathToString(path)
	if password, exists := config.ReportPasswords[pathString]; exists {
		return password
	} else {
		password = jgh.RandomString(64)
		config.ReportPasswords[pathString] = password
		return password
	}
}

func allowedAccess(password string, reportPath []string) bool {
	if password == config.MasterPassword {
		return true
	} else if password == reportPassword(reportPath) {
		return true
	} else {
		return false
	}
}

func prepareResponse(format string, path []string) {
	// path must contain a DSN and something else
	if len(path) < 2 {
		panic("path must contain at least a DSN and at least one other component")
	}

	// first component of the path is DSN
	dsn := path[0]
	path = path[1:]

	cognosInstance := cognos.New()
}
