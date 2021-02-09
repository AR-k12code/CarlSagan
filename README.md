NOTE: This version of CarlSagan is for Cognos 11. [Here is the version that works with Cognos 10](https://github.com/9072997/CarlSagan).

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
* Make a folder somewhere with
	* carlsagan.exe
	* a config.json as described later in the readme
	* usage.sqlite3 (can be an empty file)
	* a folder named "cache"
* Restrict read access to the folder so people can't access any of your files except carlsagan.exe. You can do this via IIS manager, or you can put the web.config from the end of this section in the same folder.
* Grant write permission on config.json, usage.sqlite3, and the cache folder to `FOO\IURS` where "FOO" is the server name
	* The user might be diffrent depending on your app pool identity. One way to figure out the right user is to set up everything else, then *temporarily* grant write permissions on the folder to `Everyone`. You can then try to access a page via a browser and all nessisary files and folders will be created. Note which users have permissions on these items. Don't forget to set folder permissions back.
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
Authorization can be done via HTTP Basic Auth or using the custom header `X-API-Key`. In either case you will need the master password or a report password (see config.json). To create a report password, first access the report using the master password. You can do this with a normal web browser. To see what the report password is you will have to look at the config.json file on the server. It is not available via the API at the moment.
#### HTTP Basic Auth
If authenticating with HTTP Basic Auth, you should put the master password or report password in the password field. The username does not matter. It is reported in the logs when run in standalone mode and is likely reported somewhere if you have logging set up in IIS. I recommend putting the script name in the username field for debugging.

#### X-API-Key Header
If you send a `X-API-Key` header, it will take precedence over the password sent via HTTP Basic Auth. If HTTP Basic Auth was also provided the username will be used for logging as normal, but the password will be ignored. The `X-API-Key` header should be set to the master password or a report password.

**NOTE**: Auto-generated passwords will not contain special characters, but if you set a password that does, you must take care to ensure it can be sent in this header. There is no way to escape special characters.

### URL Format
`/{namespace}/{dsn}/{root folder}/{path}?{prompt options}`

* **namespace**: The namespace is the first thing you select when signing in to Cognos in a web browser. For me this is `esp` for eSchool data or `efp` for eFinance data.
* **dsn**: For me this is `bentonvisms`. I am in the Bentonville district and sms is student management system. You will have a diffrent dsn for finance data. You can find this by opening Cognos from eschool and looking at the source code for that page. The url will include `dsn=something`. It also shows up in the URL when you edit a report.
* **root folder**: This should be either a username for paths that start in the home folder of a user in config.json or `public` for paths that start in the root public folder. For usernames containing a `\` you can use `_` instead.
* **path**: The path to the report. This is case-sensitive. You can add `.json` to the end to get json data.
* **prompt options** (optional): If your report requires you to answer prompts to run it you may specify those options as query parameters. See [report parameters](#report-parameters) for more information.

Example URL: `https://CarlSaganServer.MySchool.com/carlsagan.exe/esp/bentonvisms/APSCN_0401jpenn/scratch/complex.json`

This will give you a report called "complex" in a folder called "scratch" in the "My Folder" for the user "APSCN\\0401jpenn"

### Report Parameters
Some Cognos reports require parameters to run. For example you might be required to select a school building or a date range. There are 4 ways to specify report parameters, but they are all different ways of setting key-value pairs. For each method, the key is the `pname` of the parameter and the value is the `useValue`. In order to find the `pname`s you can just try to run the report with no parameters and you will get an error about missing a particular prompt value. Fill it in and repeat until you have all the values. Finding the `useValue` format is trickier. It often does not match the "display value". I have plans to improve this in the future, but for now you you might try looking [here](https://www.ibm.com/support/knowledgecenter/SSEP7J_11.1.0/com.ibm.swg.ba.cognos.ca_dg_cms.doc/c_rest_prompts.html#rest_prompts) to get some ideas about formatting.


#### URL Query Parameters
You can add each key-value pair as a query parameter to the URL like this
```
http://localhost:8088/esp/bentonvisms/APSCN_0401jpenn/APSCN%20Add%20Report?Building%20Parameter=8&Start%20Date%20Parameter=2019-11-01T00%3A00%3A00.000&End%20Date%20Parameter=2020-11-01T00%3A00%3A00.000
```

#### JSON
You can specify key-value pairs by using the `POST` method with a `Content-Type: application/json` request body formatted like this:
```
{
	"Building Parameter": "8",
	"Start Date Parameter": "2019-11-01T00:00:00.000",
	"End Date Parameter": "2020-11-01T00:00:00.000",
}
```

#### application/x-www-form-urlencoded
This is the normal way a browser would encode a form. Conveniently this is also the way PowerShell's `Invoke-RestMethod` encodes data if you pipe in a hash table.

#### multipart/form-data
Google it. I don't know why you would need this one, but it's there if you do.

### Response Types:
#### CSV
This is the default. Reports are the raw data as returned from Cognos.

#### JSON
You can get JSON by appending `.json` to the URL or using an `Accept: application/json` header. Reports are represented as an array of objects with each object corresponding to a row. This was designed for use with [Invoke-RestMethod](https://docs.microsoft.com/en-us/powershell/module/microsoft.powershell.utility/Invoke-RestMethod).

Each object corresponds to a row in the report. We will attempt to automatically convert to booleans or numbers, but we will only do so if we can convert all data in a column to that type. For all data types we strip trailing spaces.

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

## Caching
By default items may be served from the cache as long as they are not older than the age specified by `maxAge` in config.json. You can specify a smaller value for `maxAge` on a per-request basis using the `Cache-Control` header.
* Setting a header of `Cache-Control: max-age=600` will ensure you get data that is no more than 600 seconds (10 minutes) old.
* Setting a header of `Cache-Control: no-cache` will re-run the report regardless of how fresh the report is in the cache.
* Setting a header of `Cache-Control: only-if-cached` will always serve a report from the cache if possible. This may result in data older that the `maxAge` specified in config.json if the cache has not been cleaned out (this happens automatically).

The cache can be warmed manually based on usage. To do this run `carlsagan.exe --warm 604800` to warm all reports used in the last week (604800 seconds). If you want to reduce load during on-peek hours you can set this up as a scheduled task to run during off-peek hours.

## config.json
It should always be in the same folder as the binary and should be readable **and writeable** by the process. It will contain the infomation used to connect to cognos as well at the passwords other scripts will use to authenticate with this server. If a config.json does not exist in the same folder as the binary, it will attempt to create one. Here is an example config.json file:
```
{
	"cognosUserPasswords": {
		"APSCN\\0401jpenn": "MyExistingPasswordForCognos"
	},
	"cognosUrl": "https://adecognos.arkansas.gov",
	"reportPasswords": {
		"bentonvisms/public": "0PiolugiUkyqewq4gxQrxRPvIlkkjhgfNcwtjh88nxDkBOar1P88j68765g877MW"
	},
	"masterPassword": "ThisIsAPasswordYouMakeUp",
	"retryDelay": 3,
	"retryCount": 3,
	"httpTimeout": 30,
	"maxAge": 86400
}
```

* **cognosUserPasswords**: This is a set of usernames and passwords to connect to Cognos with. You might want to have multiple users here if you want to be able to download reports from the "My Folder" of multiple users. Reports in the public folder will use a random set of credentials out of this file. The username should be prefixed with `APSCN\` (just like when you log in using Firefox or Chrome). Note that you have to escape the `\` character in JSON. Also remember that ADE makes you change your password every 6 months or so and you will need to update it in your config when you change it.
* **cognosUrl**: If you are in Arkansas, use the same value as in the example (or `https://dev.adecognos.arkansas.gov` if you want the dev instance). This must match the protocol (http/https) used by Cognos.
* **reportPasswords**: If you are writing a config file for the first time, omit this value entirely. "report passwords" will be automatically generated when a url is accessed using the master password for the first time. This is why we need write permissions on the config file You can change these or fill them in in advance if you like, but the normal workflow is to let it generate a password then copy it into your script and never change it.
* **masterPassword**: This can be used in the same way as a report password, but it has access to all reports.
* **retryDelay**: The number of seconds to sleep after a failed request before the next retry.
* **retryCount**: The number of times a failed request to Cognos will be retried. A `retryCount` of -1 will retry forever. 
* **httpTimeout**: The maximum duration of a single request. Requests that take longer than this will be considered failed and will be retried based on the value of `retryCount`.
* **maxAge**: The default maximum age of a cache item in seconds. This can be shortened on a per-request basis using the `Cache-Control` header.
