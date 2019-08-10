# CarlSagan
A microservice that provides simple access to Arkansas' student information system via a chill, timeless API. It can run as a standalone webserver or a CGI script in IIS (probably other webservers too).

## Setup

### Standalone Webserver
`carlsagan.exe --standalone 127.0.0.1:80` to listen on port 80 on 127.0.0.1

`carlsagan.exe --standalone :80` to listen on port 80 on all IP addresses

The standalone webserver does not support HTTPS.

### CGI on IIS 10
This is how I set things up. There are lots of options for how to do this.
* Set up HTTPS
* Install CGI support under "Turn Windows features on or off"
* Make a folder somewhere with carlsagan.exe and a config.json
* Restrict read access to the folder so people can't see your config.json file. You can do this via IIS manager, or you can put the web.config from the end of this section in the same folder.
* Grant write permission on config.json to `FOO\IURS` where "FOO" is the server name
	* The user might be diffrent depending on your app pool identity. One way to figure out the right user is to set up everything else, then *temporarily* grant write permissions on the folder to `Everyone`. You can then try to access a page via a browser and a config.json will be created. Note which user's have permissions on the file. Don't forget to set folder permissions back.
 * Add the full path to carlsagan.exe to "ISAPI and CGI Restrictions" in the IIS manager
 * (optional) use the [url rewrite](https://www.iis.net/downloads/microsoft/url-rewrite) module to make your URL paths pretty. This example assumes carlsagan.exe is in the root folder and you want report paths to start at the root folder.
	 * **match url**: `^(.*)$`
	 * **action type**: Rewrite
	 * **url**: `carlsagan.exe/{R:1}`

#### web.config
Here is a sample web.config that should protect your config.json file and allow errors to be displayed
```
<?xml version="1.0" encoding="UTF-8"?>
<configuration>
	<system.webServer>
		<handlers accessPolicy="Execute, Script" />
		<httpErrors errorMode="Detailed" />
	</system.webServer>
</configuration>
```

## Using the API

### Authorization
Authorization can be done via HTTP Basic Auth or using the custom header `X-API-Key`. In either case you will need the master password or a report password (see config.json). To create a report password, first access the report using the master password. You can do this with a normal web browser (see the note about HTTP Basic Auth if running under IIS). To see what the report password is you will have to look at the config.json file on the server. It is not available via the API at the moment.
#### HTTP Basic Auth
If authenticating with HTTP Basic Auth, you should put the master password or report password in the password field. The username does not matter. It is reported in the logs when run in standalone mode and is likely reported somewhere if you have logging set up in IIS. I recommend putting the script name in the username field for debugging.

**NOTE**: IIS will block the `WWW-Authenticate` header we send to prompt the client to authenticate. Most HTTP client libraries don't need this, but the internet tells me some do. If this causes you problems, try useing the `X-API-Key` header to authenticate.

#### X-API-Key Header
If you send a `X-API-Key` header, it will take precedence over the password sent via HTTP Basic Auth. If HTTP Basic Auth was also provided the username will be used for logging as normal, but the password will be ignored. The `X-API-Key` header should be set to the master password or a report password.

**NOTE**: Auto-generated passwords will not contain special characters, but if you set a password that does, you must take care to ensure it can be sent in this header. There is no way to escape special characters.

### Response Types:
#### CSV
This is the default. Folders are represented as a list of line feed seperated names. There is no way to tell the difference between reports and folders when listing folders in this mode. Reports are the raw data as returned from Cognos.

#### JSON
You can get JSON by appending `.json` to the URL or using an `Accept` header that ends in "json". Folders are represented as a map of folder names to objects. Reports are an array of objects with each object corrisponding to a row. This was designed for use with [Invoke-RestMethod](https://docs.microsoft.com/en-us/powershell/module/microsoft.powershell.utility/Invoke-RestMethod).

##### Folders
Folders are a map of folder names to objects representing folder entries. Each object has a `type` field which may be either "report" or "folder". Additionally there is an `id` field. For folders, this is the folder id in Cognos.

Example folder:
```
{
	"Sample Complex Report": {
		"type": "report",
		"id": "CAMID(\"esp:a:0401jpenn\")/folder[@name='My Folders']/query[@name='Sample Complex Report']"
	},
	"Sample Simple Report": {
		"type": "report",
		"id": "CAMID(\"esp:a:0401jpenn\")/folder[@name='My Folders']/query[@name='Sample Simple Report']"
	},
	"Sample folder": {
		"type": "folder",
		"id": "i2A084E792C874156AG5AE644C109DDC2"
	}
}
```

##### Reports
Reports are an array of objects. Each object corresponds to a row in the report. We will attempt to automatically convert to booleans or numbers, but we will only do so if we can convert all data in a column to that type. For all data types we strip trailing spaces.

Example report:
```
[
	{
		"courseMS": "SP999",
		"average": 0.5
	},
	{
		"courseMS": "99999",
		"average": 1
	}
]
```

## config.json
It should always be in the same folder as the binary and should be readable **and writeable** by the process. It will contain the infomation used to connect to cognos as well at the passwords other scripts will use to authenticate with this server. If a config.json does not exist in the same folder as the binary, it will attempt to create one. Here is an example config.json file:
```
{
	"cognosUsername": "APSCN\\0401jpenn",
	"cognosPassword": "MyExistingPasswordForCognos",
	"cognosUrl": "https://adecognos.arkansas.gov",
	"reportPasswords": {
		"bentonvisms/~": "0PiolugiUkyqewq4gxQrxRPvIlkkjhgfNcwtjh88nxDkBOar1P88j68765g877MW"
	},
	"masterPassword": "ThisIsAPasswordYouMakeUp",
	"retryDelay": 3,
	"retryCount": 3,
	"httpTimeout": 30
}
```
* **cognosUsername**: The username you use to connect to Cognos. This should be prefixed with `APSCN\` (just like when you log in using Firefox or Chrome). Note that you have to escape the `\` character in JSON.
* **cognosPassword**: The password you use to connect to Cognos. Remember that ADE makes you change this every 6 months or so.
* **cognosUrl**: If you are in Arkansas, use the same value as in the example. This must match the protocol (http/https) used by Cognos.
* **reportPasswords**: If you are writeing a config file for the first time, omit this value entirely. "report passwords" will be automatically generated when a url is accessed using the master password for the first time. This is why we need write permissions on the config file You can change these or fill them in in advance if you like, but the normal workflow is to let it generate a password then copy it into your script and never change it.
* **masterPassword**: This can be used in the same way as a report password, but it has access to all reports.
* **retryDelay**: The number of seconds to sleep after a failed request before the next retry. It is also the polling interval when waiting for a report to finish.
* **retryCount**: The number of times a failed request to Cognos will be retried. A `retryCount` of -1 will retry forever. 
* **httpTimeout**: The maximum duration of a single request. Requests that take longer than this will be considered failed and will be retried based on the value of `retryCount`.
