package main

import(
  "os"
  "log"
 "fmt"
 "time"
 "strings"
 "strconv"

 "io/ioutil"
 "encoding/json"
 "net/http"
 "net/url"
 "database/sql"
 "github.com/mattn/go-sqlite3"

 my "jobby/server/structs"
)

func handle(err error, desc string){
  if err != nil{
    if len(os.Args) > 1{
      if os.Args[1] == "dev"{
        log.Fatal(desc, " ", err)
      }
    }
    log.Print(desc, " ", err)
  }
}

var jobs map[string]bool
func checkStarted(job string, now int64) bool {
  var ret bool
  if state, ok := jobs[job]; ok {
    ret = state
  }else{
    q, err := db.Prepare(queries["startedQuery"])
    handle(err, "Checking if " + job + " is already running")
    var tmp string
    err = q.QueryRow(job).Scan(&tmp)
    switch {
    case err == sql.ErrNoRows:
      ret = false
    case err != nil:
      ret = true
    }
    jobs[job] = ret
  }
  return ret
}

func makeInterfaces(vals []string) []interface{}{
  args := make([]interface{}, len(vals))
  for i := range vals {
    args[i] = vals[i]
  }
  return args
}

func runTxn(statements []my.Statement){
  txn, err := db.Begin()
  handle(err, "Creating a transaction")
  for _, statement := range statements{
    stmt, err := txn.Prepare(statement.Statement)
    handle(err, "Preparing " + statement.Statement)
    defer stmt.Close()
    _, err = stmt.Exec(makeInterfaces(statement.Args)...)
    handle(err, "Executing statement")
  }
  txn.Commit()
}

func runQuery(query string, vals []string) *sql.Rows {
  rows, err := db.Query(query, makeInterfaces(vals)...)
  handle(err, "Running query" + query + " for " + strings.Join(vals, ","))
  return rows
}

func parse(r *http.Request) (string, url.Values){
  return strings.Join(strings.Split(r.URL.Path, "/")[2:], "/"), r.URL.Query()
}

//TODO impl for qualitative param tracking
//Makes records of job starts/stops
func record(w http.ResponseWriter, r *http.Request){
  job, queryParams := parse(r)

  now := time.Now().UTC().UnixNano()
  //Determine if attempting to start or stop the job
  starting := strings.Split(r.URL.Path, "/")[1] == "start"
  var errString, recordStatement string
  var recordArgs []string
  if starting {
    //attempting to start the job
    errString = "Error: " + job + " already started."
    recordStatement = queries["startRecordStatement"]
    recordArgs = []string{job, strconv.FormatInt(now, 10), "-1"}
  }else{
    //attempting to stop the job
    errString = "Error: " + job + " is not running."
    recordStatement = queries["stopRecordStatement"]
    recordArgs = []string{strconv.FormatInt(now, 10), job}
  }

  if (checkStarted(job, now) && starting) || (!checkStarted(job, now) && !starting){
    //If the job is already running and attempting to start the job
    //Or if the job is not running and attempting to stop the job
    w.WriteHeader(400)
    fmt.Fprintf(w, "%s", errString)
  }else{
    statements := []my.Statement{my.Statement{recordStatement, recordArgs}}
    if len(queryParams) > 0 {
      //TODO differentiate between quant/qual param types
      statements = append(statements, my.Statement{queries["quantParamsStatement"], []string{}})
      index := 0
      for queryParam, value := range queryParams {
        if index >= 1{
          statements[1].Statement += ", union select ?, ?, ?, ?"
        }
        statements[1].Args = append(statements[1].Args, job, strconv.FormatInt(now, 10), queryParam, value[0])
        index++
      }
    }
    runTxn(statements)
    w.WriteHeader(200)
    jobs[job] = starting;
    if !starting {
      go updateJobStats(job)
    }
  }
}

//TODO finish computing jobQuantParams
func updateJobStats(job string){
  runTxn([]my.Statement{my.Statement{queries["updateJobStatsStatement"], []string{job}}})
}

func status(w http.ResponseWriter, r *http.Request){
  jobs := []my.RunningJob{}
  rows := runQuery(queries["statusQuery"], []string{})
  for rows.Next(){
    j := my.RunningJob{}
    rows.Scan(&j.Job, &j.Start, &j.Average)
    jobs = append(jobs, j)
  }
  err := rows.Err()
  handle(err, "Iterating over status query results")

  text, err := json.Marshal(jobs)
  handle(err, "Marshalling jobs into json")
  fmt.Fprintf(w, "%s", string(text))
}

func history(w http.ResponseWriter, r *http.Request){
  job, _ := parse(r)
  //TODO file github.com/mattn/sqlite3 issue for query prepping when a var is inside quotes: eg.. select * from table where col like '%?%'
  rows := runQuery(strings.Replace(queries["jobHistoryQuery"], "?", job, -1), []string{})
  jobRecords := []my.JobRecord{}
  for rows.Next(){
    j := my.JobRecord{}
    rows.Scan(&j.Job, &j.Start, &j.Stop)
    jobRecords = append(jobRecords, j)
  }

  text, err := json.Marshal(jobRecords)
  handle(err, "Marshalling records into json")
  fmt.Fprintf(w, "%s", string(text))
}

var db *sql.DB
var queries map[string]string
func init(){
  var err error
  sql.Register("sqlite3_with_extensions",
		&sqlite3.SQLiteDriver{Extensions: []string{"./extension-functions.o"}})
  db, err = sql.Open("sqlite3_with_extensions", "./jobs.db")
  handle(err, "Opening connection to sqlite")

  text, err := ioutil.ReadFile("./queries.conf")
  handle(err, "Reading query file")
  queries = make(map[string]string)
  err = json.Unmarshal(text, &queries)
  handle(err, "Unmarshaling queries into map")

  for queryName, query := range queries{
    if strings.Contains(queryName, "DDL"){
      _, err = db.Exec(query)
      handle(err, "Executing DDL: " + query)
    }
  }

  jobs = make(map[string]bool)
}

func main() {
  //static files
  http.Handle("/", http.FileServer(http.Dir("./www")))
  http.Handle("/bower_components/", http.StripPrefix("/bower_components", http.FileServer(http.Dir("./bower_components"))))

  //data services
  http.HandleFunc("/start/", record)
  http.HandleFunc("/stop/", record)
  http.HandleFunc("/history/", history)
  http.HandleFunc("/status", status)

  log.Print("Starting..")
  http.ListenAndServe(":9090", nil)
}
