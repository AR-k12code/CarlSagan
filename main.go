package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"net/http/cgi"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/9072997/jgh"
)

func handlerFunc(response http.ResponseWriter, request *http.Request) {
	// if we panic, return a 500 and log error
	success, errorMessage := jgh.Try(0, 1, false, "", func() bool {
		// we use the username field for the name of the application.
		// This is optional, but it's used for logging.
		appName, password, providedAuth := request.BasicAuth()

		// If a password was provided via the custom header, prefer that
		// one over the one from basic-auth
		apiKeyHeader := request.Header.Get("X-API-Key")
		if len(apiKeyHeader) > 0 {
			password = apiKeyHeader
			providedAuth = true
		}

		if !providedAuth {
			response.Header().Set("WWW-Authenticate", `Basic realm="Carl Sagan"`)
			response.Header().Set("Content-Type", "text/plain")
			response.WriteHeader(401)
			_, err := response.Write([]byte("Unauthorised: You must specify either the " +
				"master password or a report password in the password " +
				"field via HTTP basic auth or via the X-API-Key header.\n"))
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
			response.Header().Set("Content-Type", "text/plain")
			response.WriteHeader(401)
			_, err := response.Write([]byte("Unauthorised: The provided password " +
				"is invalid or does not provide access to the requested " +
				"resource\n"))
			jgh.PanicOnErr(err)
			return true
		}

		// determine the max age allowed by the request headers.
		ccHeader := strings.ToLower(request.Header.Get("Cache-Control"))
		var maxAge uint
		if ccHeader == "" {
			// default to value from config.
			config.mutex.Lock()
			maxAge = config.MaxAge
			config.mutex.Unlock()
		} else if ccHeader == "no-cache" {
			maxAge = 0
		} else if ccHeader == "only-if-cached" {
			maxAge = math.MaxInt32
		} else if strings.HasPrefix(ccHeader, "max-age=") {
			maxAgeStr := strings.TrimPrefix(ccHeader, "max-age=")
			maxAgeStr = strings.TrimSpace(maxAgeStr)
			maxAge64, err := strconv.ParseInt(maxAgeStr, 10, 32)
			if err != nil || maxAge64 < 0 {
				response.Header().Set("Content-Type", "text/plain")
				response.WriteHeader(400)
				_, err := response.Write([]byte("The server did not understand your max-age header\n"))
				jgh.PanicOnErr(err)
				return true
			}
			maxAge = uint(maxAge64)
		} else {
			response.Header().Set("Content-Type", "text/plain")
			response.WriteHeader(501)
			_, err := response.Write([]byte("Requested Cache-Control method not implimented\n"))
			jgh.PanicOnErr(err)
			return true
		}

		// do the cognos requests
		respBody := PrepareResponse(asJSON, path, maxAge)

		// set the content type
		if asJSON {
			response.Header().Set("Content-Type", "application/json")
		} else {
			response.Header().Set("Content-Type", "text/csv")
			// for CSV we specify a filename so I can give links to users for use in a browser.
			// only allow charicters in the filename that I won't have to quote in the HTTP header
			safeReportName := regexp.MustCompile("[^A-Za-z0-9 _.-]").ReplaceAllString((path[lastPathPos]), "")
			response.Header().Set("Content-Disposition", `attachment; filename="`+safeReportName+`.csv"`)
		}
		// set content length
		// this is not required, but lets browsers display progress
		contentLength := strconv.FormatInt(int64(len([]byte(respBody))), 10)
		response.Header().Set("Content-Length", contentLength)
		// send actual data
		_, err := response.Write([]byte(respBody))
		jgh.PanicOnErr(err)
		return true
	})
	if !success {
		response.Header().Set("Content-Type", "text/plain")
		response.WriteHeader(500)
		errRespBody := fmt.Sprintf("%v\n", errorMessage)
		_, err := response.Write([]byte(errRespBody))
		jgh.PanicOnErr(err)
	}
}

func loadConfigFixedLocation() {
	// get the directory of this executable
	exePath, err := os.Executable()
	jgh.PanicOnErr(err)
	exeFolder := filepath.Dir(exePath)

	configPath := exeFolder + "/config.json"

	// IDK what this is, but it happens in IIS
	configPath = strings.TrimPrefix(configPath, `\\?\`)

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
		runningAsCGI = true
		err := cgi.Serve(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			success, errorMessage := jgh.Try(0, 1, false, "", func() bool {
				// trim the path to the CGI off our request path
				cgiPrefix := os.Getenv("SCRIPT_NAME")
				request.URL.Path = strings.TrimPrefix(request.URL.Path, cgiPrefix)

				// load the global config
				loadConfigFixedLocation()
				return true
			})
			if !success {
				response.Header().Set("Content-Type", "text/plain")
				response.WriteHeader(500)
				errRespBody := fmt.Sprintf("%v\n", errorMessage)
				_, err := response.Write([]byte(errRespBody))
				jgh.PanicOnErr(err)
				return
			}

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
