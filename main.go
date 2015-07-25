package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"database/sql"
	"encoding/json"
	"encoding/xml"
	"flag"

	"html/template"

	_ "github.com/mattn/go-sqlite3"
	"github.com/robfig/config"
)

var conf *config.Config
var configFile = flag.String("c", "corp_member_tracking.conf", "specify config file")

type NotificationHeaders struct {
	Notifications []Notifications `xml:"result>rowset>row"`
}

func formatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

var tFuncMap = template.FuncMap{
	"datetime": formatTime,
}

type Notifications struct {
	NotificationID int64  `xml:"notificationID,attr"`
	TypeID         int    `xml:"typeID,attr"`
	SenderID       int64  `xml:"senderID,attr"`
	SenderName     string `xml:"senderName,attr"`
	SentDate       string `xml:"sentDate,attr"`
	Read           int64  `xml:"read,attr"`
}

type Records struct {
	Events []InsertData
}
type InsertData struct {
	CharID             int64
	Date               time.Time
	NotificationID     int64
	NotificationTypeID int
	SenderName         string
}

func main() {
	flag.Parse()

	conf, err := config.ReadDefault(*configFile)
	if err != nil {
		conf = config.NewDefault()
		fmt.Printf("Error loading config file")
	}

	log.SetFlags(log.Ldate | log.Ltime)
	logDir, _ := conf.String("DEFAULT", "log_dir")
	if logDir == "" {
		logDir = "."
	}

	filePrefix := "twitch_stats-"
	fnTime := time.Now().UTC().Format("200601")

	logFile := fmt.Sprintf("%s/%s%s.log", logDir, filePrefix, fnTime)
	fp, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_SYNC|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("Failed to open log file '%s': %s", logFile, err)
	}

	log.SetOutput(fp)

	keyid, _ := conf.Int("DEFAULT", "KeyID")
	vcode, _ := conf.String("DEFAULT", "vCode")
	listenOn, _ := conf.String("DEFAULT", "listen")

	initDB()

	//Setup the timer. This pulls notifications from the API every 21 minutes. The cache timer is 20 minutes, so it should be ok.
	//Some endeavouring dev could probably modify this so that it gets the cache timer from the API and checks at that time, but :effort:
	t := time.NewTicker(time.Minute * 21).C
	go func() {
		for {
			select {
			case <-t:
				getNotifications(keyid, vcode)
			}
		}
	}()

	//This is the part that listens and handles the incoming http connections.
	http.HandleFunc("/", getStats)

	//This part is so that the static files actually load, mainly the css and js shit.
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	http.ListenAndServe(listenOn, nil)

}

func getNotifications(keyid int, vcode string) {
	ids := &Records{}

	//Make the request to the API
	req, err := http.NewRequest("GET", fmt.Sprintf("https://api.eveonline.com/Char/Notifications.xml.aspx?keyid=%d&vcode=%s", keyid, vcode), nil)
	client := &http.Client{}
	res, err := client.Do(req)

	defer res.Body.Close()

	if err != nil {
		log.Printf("Error getting Header API data: %s", err)
		return
	}

	if res.StatusCode != 200 {
		log.Printf("Header API returned non 200 response: %d", res.StatusCode)
		return
	}

	//Now that we have the content from CCP and we have made sure we got a valid result, lets decode it into something we can work with
	d := xml.NewDecoder(res.Body)
	decoded := &NotificationHeaders{}
	err = d.Decode(&decoded)

	if err != nil {
		log.Printf("Error decoding header API data: %s", err)
	}

	//Now that its decoded, lets loop through the notifications and pull out the joins (128) and leaves (21)
	for _, v := range decoded.Notifications {
		if v.TypeID == 21 || v.TypeID == 128 {
			sentDate, err := time.Parse("2006-01-02 15:04:05", v.SentDate)
			if err != nil {
				log.Printf("Error parsing time: %s", err)
			}
			ids.Events = append(ids.Events, InsertData{CharID: v.SenderID, SenderName: v.SenderName, NotificationID: v.NotificationID, NotificationTypeID: v.TypeID, Date: sentDate})
		}
	}

	//Now that we have only the notifications we care about, lets store it in the db.
	storeData(ids)
}

func initDB() {
	//Initialize the DB connection. Will create the file if it does not exist
	db, err := sql.Open("sqlite3", "./corp_member_tracking.db")
	if err != nil {
		log.Printf("Error creating database: %s", err)
	}

	defer db.Close()

	sql := `CREATE TABLE IF NOT EXISTS member_tracking (
			notificationID integer not null primary key,
			charID	integer not null,
			charName	text not null,
			notificationTypeID integer not null,
			eventDate	timestamp not null
		)`

	//Setup the table if it doesn't already exist
	_, err = db.Exec(sql)
	if err != nil {
		log.Printf("Error creating table: %s", err)
	}
}

func storeData(data *Records) {
	//Open the connection to the db file
	db, err := sql.Open("sqlite3", "./corp_member_tracking.db")
	if err != nil {
		log.Printf("Error opening database: %s", err)
	}

	//Lets loop the data passed to us and store it
	for _, v := range data.Events {
		stmt, err := db.Prepare("INSERT OR REPLACE INTO member_tracking (notificationID, charID, charName, notificationTypeID, eventDate) VALUES (?,?,?,?,?);")
		if err != nil {
			log.Printf("Error preparing SQL SELECT: %s", err)
			continue
		}
		_, err = stmt.Exec(v.NotificationID, v.CharID, v.SenderName, v.NotificationTypeID, v.Date)
		if err != nil {
			log.Printf("Error inserting data: %s", err)
			continue
		}
	}

	db.Close()
}

func getStats(w http.ResponseWriter, r *http.Request) {
	type Char struct {
		CharID    int64
		CharName  string
		EventDate time.Time
	}

	type ChartData struct {
		Categories template.JS
		Join       template.JS
		Leave      template.JS
	}

	type TmplData struct {
		Title      string
		Joins      []Char
		Leaves     []Char
		Date       string
		JoinCount  int
		LeaveCount int
		ChartData  ChartData
	}

	q := r.URL.Path[1:]
	if len(q) <= 0 {
		q = time.Now().UTC().Format("2006-01")
	}

	tmplData := TmplData{Title: "Member Stats", Date: q}

	db, err := sql.Open("sqlite3", "./corp_member_tracking.db")
	if err != nil {
		log.Printf("Error opening database: %s", err)
	}
	defer db.Close()

	sql := `SELECT notificationID, charID, charName, notificationTypeID, eventDate FROM member_tracking WHERE strftime("%Y-%m", eventDate) = ?;`

	stmt, err := db.Prepare(sql)
	if err != nil {
		log.Printf("Error preparing statement: %s", err)
		return
	}

	rows, err := stmt.Query(q)
	if err != nil {
		log.Printf("There was an error executing the statement: %s")
		return
	}

	defer rows.Close()
	j := 0
	l := 0
	for rows.Next() {
		var notificationID, charID int64
		var charName string
		var eventDate time.Time
		var notificationTypeID int

		err = rows.Scan(&notificationID, &charID, &charName, &notificationTypeID, &eventDate)
		if err != nil {
			log.Printf("Scan error: %s", err)
			continue
		}

		if notificationTypeID == 21 {
			l = l + 1
			tmplData.Leaves = append(tmplData.Leaves, Char{CharID: charID, CharName: charName, EventDate: eventDate})
		}
		if notificationTypeID == 128 {
			j = j + 1
			tmplData.Joins = append(tmplData.Joins, Char{CharID: charID, CharName: charName, EventDate: eventDate})
		}
	}
	tmplData.JoinCount = j
	tmplData.LeaveCount = l

	//This whole next section is a giant clusterfuck, but it prints out a nice pretty graph!

	sql = `SELECT strftime("%m-%d",eventDate) AS day, notificationTypeID, COUNT(*) as count
FROM member_tracking
WHERE strftime("%Y-%m", eventDate) = ?
GROUP BY strftime("%m-%d",eventDate), notificationTypeID
ORDER BY day ASC`

	stmt, err = db.Prepare(sql)
	if err != nil {
		log.Printf("Error preparing statement: %s", err)
		return
	}

	rows, err = stmt.Query(q)
	if err != nil {
		log.Printf("There was an error executing the statement: %s")
		return
	}

	defer rows.Close()

	var day string
	var notificationTypeID, count int

	joinMap := make(map[string]int)
	leaveMap := make(map[string]int)
	i := 0
	for rows.Next() {
		err = rows.Scan(&day, &notificationTypeID, &count)
		if err != nil {
			log.Printf("Scan error: %s", err)
			continue
		}
		joinMap[day] = 0
		leaveMap[day] = 0

		if notificationTypeID == 21 {
			leaveMap[day] = count
		}
		if notificationTypeID == 128 {
			joinMap[day] = count
		}
		i = i + 1
	}

	categories := make([]string, len(joinMap))
	joinData := make([]int, len(joinMap))
	leaveData := make([]int, len(joinMap))

	i = 0
	for k, v := range joinMap {
		joinData[i] = v
		categories[i] = k
		i = i + 1
	}
	i = 0
	for _, v := range leaveMap {
		leaveData[i] = v
		i = i + 1
	}

	cat, _ := json.Marshal(categories)
	tmplData.ChartData.Categories = template.JS(cat)
	join, _ := json.Marshal(joinData)
	tmplData.ChartData.Join = template.JS(join)
	leave, _ := json.Marshal(leaveData)
	tmplData.ChartData.Leave = template.JS(leave)

	//This is where the clusterfuck ends.

	mainTemplate, err := template.New("template.html").Funcs(tFuncMap).ParseFiles("template.html")
	if err != nil {
		log.Printf("Template error: %s", err)
		return
	}
	err = mainTemplate.Execute(w, tmplData)
	if err != nil {
		log.Printf("Template Error: %s", err)
	}
}
