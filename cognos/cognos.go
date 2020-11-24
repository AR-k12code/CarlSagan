// This is for accessing The Arkansas Department of Education Cognos system.
// it might also work for other Cognos installations. It can list directories.
// and run/download reports (that have already been built) synchronously to
// CSV strings. Basically everything panics on failure. I use a helper function
// called Try() to handle these panics (http://github.com/9072997/jgh).
package cognos

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/9072997/jgh"
	"github.com/Azure/go-ntlmssp"
	"golang.org/x/net/publicsuffix"
	"golang.org/x/sync/semaphore"
)

// Session Represents a Cognos connection with a single namespace & dsn
type Session struct {
	User         string
	Pass         string
	URL          string
	Namespace    string
	DSN          string
	RetryDelay   uint
	RetryCount   int
	accountID    string
	client       http.Client
	httpLockPool *semaphore.Weighted
}

// used for a objects used in the API
type namespaceAndDSN struct {
	Parameters []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"parameters"`
}
type directoryListing struct {
	XMLName xml.Name         `xml:"inspection"`
	Entries []directoryEntry `xml:"link"`
}
type directoryEntry struct {
	Location string `xml:"location,attr"`
	Name     string `xml:"abstract"`
}

// return the JSON payload necessary to set the namespace and DSN
func makeNamespaceAndDSN(namespace, dsn string) string {
	var n namespaceAndDSN
	jgh.InitSlice(&n.Parameters, 3)

	n.Parameters[0].Name = "h_CAM_action"
	n.Parameters[0].Value = "logonAs"
	n.Parameters[1].Name = "CAMNamespace"
	n.Parameters[1].Value = namespace
	n.Parameters[2].Name = "dsn"
	n.Parameters[2].Value = dsn

	payloadStr, err := json.Marshal(n)
	jgh.PanicOnErr(err)

	return string(payloadStr)
}

// MakeInstance creates a new cognos object.
// user is the user used to connect to Cognos (ex: APSCN\0401jpenn).
// This value also changes which "my folders" folder ~ points to.
// url is the base URL of the cognos server (ex: https://adecognos.arkansas.gov).
// namespace is the first thing you choose when signing in to Cognos.
// I don't totally know what dsn is, but mine is bentonvisms.
// If you open cognos in eschool and view source, you can see this value in the URL for the iframe.
// There is a diffrent one for eschool and e-finance.
// retryDelay is the number of seconds before a failed request will be retried.
// It is also the polling interval when waiting for a report to finish.
// retryCount is the number of times a failed request will be retried.
// A retryCount of -1 will retry forever.
// httpTimeout is the number seconds before giving up on a Cognos HTTP request.
// concurrentRequests limits the maximum number of requests going at once.
func MakeInstance(
	user, pass, url, namespace, dsn string,
	retryDelay uint,
	retryCount int,
	httpTimeout uint,
	concurrentRequests uint,
	transport http.RoundTripper,
) (c Session) {
	c = Session{
		User:         user,
		Pass:         pass,
		URL:          url,
		Namespace:    namespace,
		DSN:          dsn,
		RetryDelay:   retryDelay,
		RetryCount:   retryCount,
		httpLockPool: semaphore.NewWeighted(int64(concurrentRequests)),
	}

	// make a new cookie jar
	// (cookie jars are threadsafe)
	jar, err := cookiejar.New(
		&cookiejar.Options{
			PublicSuffixList: publicsuffix.List,
		},
	)
	jgh.PanicOnErr(err)

	// if no transport was provided, make a normal one
	if transport == nil {
		transport = new(http.Transport)
	}

	// make a httpClient that uses the cookie jar and supports NTLM auth
	c.client = http.Client{
		Jar:     jar,
		Timeout: time.Duration(httpTimeout) * time.Second,
		Transport: ntlmssp.Negotiator{
			RoundTripper: transport,
		},
	}

	// set the namespace/DSN I don't think this is an official, documented
	// part of the API. It's just what I observed a browser doing.
	c.Request(
		"POST",
		"/ibmcognos/bi/v1/login",
		makeNamespaceAndDSN(c.Namespace, c.DSN),
	)

	// find account ID (needed to get reports from "My Folders")
	c.accountID = c.currentAccountID()

	return
}

// find the account ID of the current user. Ex:
// CAMID("esp:a:0401jpenn")
func (c Session) currentAccountID() string {
	// list all available directories
	resp := c.Request(
		"GET",
		"/ibmcognos/bi/v1/disp/rds/wsil",
		"",
	)

	// unmarshal response into an object
	var dir directoryListing
	err := xml.Unmarshal([]byte(resp), &dir)
	jgh.PanicOnErr(err)

	// find the entry for "My Folders"
	var myFolder *directoryEntry
	for _, entry := range dir.Entries {
		if entry.Name == "My Folders" {
			myFolder = &entry
			break
		}
	}
	if myFolder == nil {
		panic("Could not find My Folders")
	}

	// parse account ID from link
	// it's the 2nd to the last path component
	link, err := url.PathUnescape(myFolder.Location)
	jgh.PanicOnErr(err)
	pathComponents := strings.Split(link, "/")
	return pathComponents[len(pathComponents)-2]
}

// Request makes a HTTP GET request to the link (not including hostname)
// provided via the "link" parameter. The response body is returned as a string.
// Any errors (including a non-200 response) will cause this function to panic.
func (c Session) Request(method string, link string, reqBody string) (respBody string) {
	// limit concurrent requests
	// background means don't give up waiting for lock
	err := c.httpLockPool.Acquire(context.Background(), 1)
	jgh.PanicOnErr(err)
	defer c.httpLockPool.Release(1)

	// it never makes sense to have a try count of 0, so we ask the user
	// for retry count and convert it
	var tryCount int
	if c.RetryCount < 0 {
		tryCount = -1
	} else {
		tryCount = c.RetryCount + 1
	}

	success, _ := jgh.Try(int(c.RetryDelay), tryCount, true, "", func() bool {
		// make an io.reader if we have post data
		var reqBodyReader io.Reader
		if len(reqBody) > 0 {
			reqBodyReader = strings.NewReader(reqBody)
		} else {
			reqBodyReader = nil
		}

		// set up and send a GET request (no body)
		req, err := http.NewRequest(method, c.URL+link, reqBodyReader)
		jgh.PanicOnErr(err)

		// auth used to get past the reverse proxy
		req.SetBasicAuth(c.User, c.Pass)

		// set POST body mime type automatically
		if len(reqBody) > 0 {
			// so far I have only had to deal with 2 mime types, so I am not
			// going to make thing complicated. If it starts with a "{",
			// it's json. Otherwise it's application/x-www-form-urlencoded
			var mimeType string
			if reqBody[0] == '{' {
				mimeType = "application/json"
			} else {
				mimeType = "application/x-www-form-urlencoded"
			}
			req.Header.Set("Content-Type", mimeType)
		}

		// Does this help?? TODO
		req.Header.Set("Accept", "*/*")
		req.Header.Set("User-Agent", "insomnia/2020.4.2")

		resp, err := c.client.Do(req)
		jgh.PanicOnErr(err)
		defer resp.Body.Close()

		respBody = jgh.ReadAll(resp.Body)

		// check HTTP response code
		if resp.StatusCode != 200 {
			panic("Error from Cognos: " + resp.Status + ":" + respBody)
		}

		return true
	})
	if !success {
		panic("Cognos request to " + link + " failed.")
	}
	return respBody
}

// escape a path component based on rules from tinyurl.com/y58pzsy3
func cognosEscape(pathComponent string) string {
	// "_" becomes "_x005F"
	// this is generalized from the docs to handle "__"
	pathComponent = strings.ReplaceAll(pathComponent, "_", "_x005F")

	// " " becomes "__"
	pathComponent = strings.ReplaceAll(pathComponent, " ", "__")

	// boaring characters can stay (a-z, A-Z, 0-9, and _ since we dealt with
	// it already) but anything even slightly weird gets encoded to _x0000
	// where 0000 is the character's hex representation in UTF16
	weirdChars := regexp.MustCompile("[^a-zA-Z0-9_]")
	pathComponent = weirdChars.ReplaceAllStringFunc(
		pathComponent,
		func(s string) string {
			r := []rune(s)[0]
			return fmt.Sprintf("_x%04X", r)
		},
	)

	return pathComponent
}

func (c Session) encodePath(path []string) string {
	if len(path) < 2 {
		panic("Path must contain at least 2 components")
	}

	// we want to point to my folders for the current user
	if path[0] == "~" {
		// path[0] contains "~"
		// we need to replace that with 2 components
		// * c.accountID
		// * "My Folders"
		path = append(
			[]string{c.accountID, "My Folders"},
			path[1:]...,
		)
	}

	for i := range path {
		path[i] = cognosEscape(path[i])
	}
	return strings.Join(path, "/")
}

// DownloadReportCSV returns a string containing CSV data for a cognos report.
// This function triggers the execution of the report, and may take a while
// to return.
func (c Session) DownloadReportCSV(path []string) string {
	reportURL := "/ibmcognos/bi/v1/disp/rds/outputFormat/path/" +
		c.encodePath(path) + "/CSV?async=OFF"
	return c.Request("GET", reportURL, "")
}
