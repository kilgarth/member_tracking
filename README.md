# Corporation Member Tracking
This tool is designed to pull notifications from a CEO character via API and log all of the notifications related to characters joining and leaving the corporation

The application will store all of the data in an sqlite3 db and has its own built in webserver to show the statistics, or you can use the sqlite file in some other application if you are so inclined.

# Installing
You must have Go installed on the machine you are building. You can build the app and then use just the binary along with all of the other files, if you want. 

Pull the code and run "go get" and then "go build". This will create a binary called "corp_member_tracking". Make sure it is executable, and then run it.

Alternatively, if you are lazy and using linux, I have included a compiled binary.

# Configuration
In the config file there are some things that need to be changed.
1. You must set the API information (KeyID and vCode). The API must be a CEO API with at least Notifications enabled.
2. (Optional) You can set the log directory. If nothing is set, the base directory will be used.
3. (Optional) You can change the ip/port the webserver listens on. The default is all IP's, port 5555

# Usage
Run the application. Once it is running, you can point your browser to whatever you set it to listen on (default is localhost:5555).
This will show the stats for the current month. If you wish to change the month it is showing stats for, put it in YYYY-MM format in the URL.
Example: http://localhost:5555/2015-06 would tell it to look for June 2015 stats.