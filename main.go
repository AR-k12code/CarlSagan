package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/cgi"
	"os"
	"path/filepath"
	"strings"

	"github.com/9072997/jgh"
)

func handlerFunc(response http.ResponseWriter, request *http.Request) {
	// if we panic, return a 500 and log error
	success, errorMessage := jgh.Try(0, 1, false, "", func() bool {
		// we use the username field for the name of the application.
		// This is optional, but it's used for logging.
		appName, password, providedBasicAuth := request.BasicAuth()

		if !providedBasicAuth {
			response.Header().Set("WWW-Authenticate", `Basic realm="Carl Sagan"`)
			response.WriteHeader(401)
			_, err := response.Write([]byte("Unauthorised: You must specify either the " +
				"master password or a report password in the password " +
				"field via HTTP basic auth\n"))
			jgh.PanicOnErr(err)
			return true
		}

		if !runningAsCGI {
			log.Println("Request for", request.URL.Path, "from", appName)
		}

		path := ParsePath(request.URL.Path)

		// did the client want csv or json
		// check this before authorization in case we need to strip .json
		asJSON := false // default to CSV
		// check "Accept" header
		acceptMimeType := request.Header.Get("Accept")
		if strings.HasSuffix(acceptMimeType, "json") {
			asJSON = true
		}
		// check for .json in last path component
		lastPathPos := len(path) - 1
		if strings.HasSuffix(path[lastPathPos], ".json") {
			asJSON = true
			// remove the .json from the path
			path[lastPathPos] = strings.TrimSuffix(path[lastPathPos], ".json")
		}

		// check if the password is valid
		if !AllowedAccess(password, path) {
			response.Header().Set("WWW-Authenticate", `Basic realm="Carl Sagan"`)
			response.WriteHeader(401)
			_, err := response.Write([]byte("Unauthorised: The provided password " +
				"is invalid or does not provide access to the requested " +
				"resource\n"))
			jgh.PanicOnErr(err)
			return true
		}

		// do the cognos requests and send the result
		respBody := PrepareResponse(asJSON, path)
		_, err := response.Write([]byte(respBody))
		jgh.PanicOnErr(err)

		return true
	})
	if !success {
		response.WriteHeader(500)
		_, err := response.Write([]byte(fmt.Sprintf("%v\n", errorMessage)))
		jgh.PanicOnErr(err)
	}
}

func loadConfigFixedLocation() {
	// get the directory of this executable
	exePath, err := os.Executable()
	jgh.PanicOnErr(err)
	exeFolder := filepath.Dir(exePath)

	configPath := exeFolder + "/config.json"

	config.mutex.Lock()
	defer config.mutex.Unlock()
	readConfig(configPath)

	// save the path to the config so we can write it out later if it
	//  is modified
	config.configPath = configPath
}

var runningAsCGI = false

func main() {
	if len(os.Args) == 3 && os.Args[1] == "--standalone" {
		// use built-in webserver
		// load config
		loadConfigFixedLocation()

		// print a warning about no encryption
		fmt.Println("WARNING: You are using the standalone webserver. It does not support TLS.")

		// start the webserver
		http.HandleFunc("/", handlerFunc)
		port := os.Args[2]
		err := http.ListenAndServe(port, nil)
		jgh.PanicOnErr(err)
	} else if len(os.Args) == 1 {
		// cgi
		// load config
		loadConfigFixedLocation()

		// handle request
		runningAsCGI = true
		err := cgi.Serve(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			handlerFunc(response, request)
		}))
		jgh.PanicOnErr(err)
	} else {
		// invalid args; print usage information
		fmt.Println("Usage:", os.Args[0], "--standalone [ip address]:<port>")
		fmt.Println("Examples:", os.Args[0], "--standalone :8080")
		fmt.Println("         ", os.Args[0], "--standalone 127.0.0.1:8080")
		fmt.Println("This executable also supports CGI.")
		fmt.Println()
		fmt.Println("Put a file named config.json in the same directory as the executable.")
		fmt.Println("For information on what should go in this file, see the documentation.")
	}
}
