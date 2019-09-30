package main

import (
	"encoding/csv"
	"encoding/json"
	"io/ioutil"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/9072997/cognos"
	"github.com/9072997/jgh"
	"github.com/iancoleman/strcase"
)

var config struct {
	CognosUserPasswords map[string]string `json:"cognosUserPasswords"`
	CognosURL           string            `json:"cognosUrl"`
	ReportPasswords     map[string]string `json:"reportPasswords"`
	MasterPassword      string            `json:"masterPassword"`
	RetryDelay          uint              `json:"retryDelay"`
	RetryCount          int               `json:"retryCount"`
	HTTPTimeout         uint              `json:"httpTimeout"`
	configPath          string
	mutex               sync.Mutex
}

const minMsForPasswordCheck = 100

// Lock the mutex before calling
func readConfig(filename string) {
	configJSON, err := ioutil.ReadFile(filename)
	if err != nil {
		// make a spot for 1 user in the config template
		config.CognosUserPasswords = map[string]string{"": ""}

		writeConfig(filename)
		panic("No config file was found. One has been created in the " +
			"same folder as this executable")
	}

	err = json.Unmarshal(configJSON, &config)
	jgh.PanicOnErr(err)

	// make sure we have at least 1 Cognos user
	if len(config.CognosUserPasswords) == 0 {
		panic("You must specify at least 1 Cognos user in the config file")
	}

	// make sure we don't have a nil map
	if config.ReportPasswords == nil {
		config.ReportPasswords = make(map[string]string)
	}
}

// Lock the mutex before calling
func writeConfig(filename string) {
	configJSON, err := json.MarshalIndent(config, "", "\t") //nolint
	jgh.PanicOnErr(err)
	err = ioutil.WriteFile(filename, configJSON, 0600)
	jgh.PanicOnErr(err)
}

func pathToString(path []string) string {
	// check that no path components contain a slash
	if strings.Contains(strings.Join(path, ""), "/") {
		panic("A path component may not contain a slash")
	}

	return strings.Join(path, "/")
}

// Lock the mutex before calling
func createReportPassword(path []string) {
	pathString := pathToString(path)

	// panic if we were asked to create a password for a
	// report that already has one
	if _, exists := config.ReportPasswords[pathString]; exists {
		panic("Report password already exists for given path")
	}

	password := jgh.RandomString(64)
	config.ReportPasswords[pathString] = password

	// we modified the config, save it back to disk
	writeConfig(config.configPath)
}

// Lock the mutex before calling
func reportPassword(path []string) (hasPassword bool, password string) {
	pathString := pathToString(path)
	password, exists := config.ReportPasswords[pathString]
	return exists, password
}

// this checks is a password is valid for a given path. If the master
// password is used to authenticate to a previously unknown report, a
// report password will be generated. It has a minimum execution time of
// 100ms to guard against timeing attacks
func AllowedAccess(providedPassword string, reportPath []string) (allowed bool) {
	config.mutex.Lock()

	// we use a wait group to enforce a minimum execution time to
	// prevent timeing attacks
	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	go func() {
		time.Sleep(time.Millisecond * minMsForPasswordCheck)
		waitGroup.Done()
	}()

	// do the actual check
	allowed = false
	hasReportPassword, password := reportPassword(reportPath)
	if hasReportPassword && providedPassword == password {
		allowed = true
	} else if providedPassword == config.MasterPassword {
		allowed = true
		// if authenticated with the master password and there is
		// not a report password yet, create one
		if !hasReportPassword {
			createReportPassword(reportPath)
		}
	}
	config.mutex.Unlock()

	// wait for out minimum time
	waitGroup.Wait()
	return
}

type dataTypeType uint

const (
	boolType    dataTypeType = iota
	int64Type   dataTypeType = iota
	float64Type dataTypeType = iota
	stringType  dataTypeType = iota
)

func sliceType(data []string) (dataType dataTypeType) {
	// start by assuming the column is a bool
	// this is the 0 value for dataTypeType, so we don't need to do anything
	// if there is
	for i := 0; i < len(data); i++ {
		// remove trailing spaces
		data[i] = strings.TrimRight(data[i], " ")

		var err error
		// try to parse as current type
		switch dataType {
		case boolType:
			_, err = strconv.ParseBool(data[i])
		case int64Type:
			_, err = strconv.ParseInt(data[i], 10, 64)
		case float64Type:
			_, err = strconv.ParseFloat(data[i], 64)
		case stringType:
			// all strings are valid strings. we don't have to check.
			return stringType
		}

		// if we failed to parse, move to next type and try again
		if err != nil {
			dataType++
			i = 0
		}
	}

	return
}

func csvToJSON(csvData string) string {
	// parse CSV
	csvReader := csv.NewReader(strings.NewReader(csvData))
	data, err := csvReader.ReadAll()
	jgh.PanicOnErr(err)

	// we need atleast a header row
	if len(data) < 1 {
		panic("Need at least 1 row to parse CSV")
	}

	// seperate header row from data
	headers := data[0]
	rows := data[1:]

	// determine the type of each column
	var colTypes []dataTypeType
	for colNum := range headers {
		// build a colum slice from our rows
		var column []string
		for _, row := range rows {
			// don't panic if a row is missing a field
			// just don't consider it when determining type
			jgh.Try(0, 1, false, "", func() bool {
				column = append(column, row[colNum])
				return true
			})
		}
		colTypes = append(colTypes, sliceType(column))
	}

	// build the slice that will eventuially be marshaled to JSON
	var dataObjects []map[string]interface{}
	for _, row := range rows {
		dataObject := make(map[string]interface{})

		// build a single data object
		for colNum, colName := range headers {
			// remove trailing spaces
			value := strings.TrimRight(row[colNum], " ")

			// canonicalize column names
			// BUG(jon): what if there are duplicate names
			colName = strcase.ToLowerCamel(colName)
			// don't panic if a row is missing a field
			jgh.Try(0, 1, false, "", func() bool {
				// we don't have to check errors here because we already
				// checked in sliceType() that these convert cleanly
				switch colTypes[colNum] {
				case boolType:
					dataObject[colName], _ = strconv.ParseBool(value)
				case int64Type:
					dataObject[colName], _ = strconv.ParseInt(value, 10, 64)
				case float64Type:
					dataObject[colName], _ = strconv.ParseFloat(value, 64)
				case stringType:
					dataObject[colName] = value
				}
				return true
			})
		}

		dataObjects = append(dataObjects, dataObject)
	}

	// marshal into JSON
	jsonData, err := json.MarshalIndent(dataObjects, "", "\t")
	jgh.PanicOnErr(err)

	return string(jsonData)
}

func PrepareResponse(asJSON bool, path []string) (response string) {
	// path must contain a DSN and something else
	if len(path) < 2 {
		panic("path must contain a DSN and at least one other component")
	}

	// first component of the path is DSN
	// extract the dsn and remove it from the path
	dsn := path[0]
	path = path[1:]

	config.mutex.Lock()

	// the next component of the path is either a username or "public".
	// if it is a username, we need to set the user/password and change
	// the root to "~". A "~" indicates "the current user's home folder"
	// to our library.
	var username, password string
	if path[0] == "public" {
		// grab any set of Cognos credentials
		for username, password = range config.CognosUserPasswords {
			break
		}
	} else {
		// usernames have backslashes in them, but putting one of those
		// in a URL is awkward, so we allow using "_" insted
		username = strings.Replace(path[0], "_", `\`, 1)

		var userInConfig bool
		password, userInConfig = config.CognosUserPasswords[username]
		if !userInConfig {
			panic("no password for " + username + " in config file")
		}

		// our library expects "~" for the current user's folder
		path[0] = "~"
	}

	cognosInstance := cognos.MakeInstance(
		username,
		password,
		config.CognosURL,
		dsn,
		config.RetryDelay,
		config.RetryCount,
		config.HTTPTimeout,
		1,
	)

	// talking to cognos can take a long time, so unlock before we start
	config.mutex.Unlock()

	// walk the folder structure to get to the thing referenced by path
	object := cognosInstance.FolderEntryFromPath(path)

	if object.Type == cognos.Folder {
		folderEntries := cognosInstance.LsFolder(object.ID)
		if asJSON {
			jsonEntries, err := json.MarshalIndent(folderEntries, "", "\t")
			jgh.PanicOnErr(err)
			return string(jsonEntries)
		} else {
			// just a newline seperated list
			for name := range folderEntries {
				response += name + "\n"
			}
			return
		}
	} else if object.Type == cognos.Report {
		reportCSV := cognosInstance.DownloadReportCSV(object.ID)
		if asJSON {
			return csvToJSON(reportCSV)
		} else {
			// just return the CSV data as is from cognos
			return reportCSV
		}
	} else {
		panic("Got folder entry of unknown type")
	}
}

func ParsePath(path string) []string {
	path = strings.Trim(path, "/")
	return strings.Split(path, "/")
}
