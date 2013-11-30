package main

import(
  "os"
  "log"
 "fmt"
 "time"
 "strings"
 "strconv"

 "encoding/json"
 "io/ioutil"
 "net/http"
 "net/url"
 "database/sql"
 "github.com/mattn/go-sqlite3"
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

var files map[string]string
func static(w http.ResponseWriter, r *http.Request) {
  fn := "." + r.URL.Path
  if _, ok := files[fn]; !ok {
    text, err := ioutil.ReadFile(fn)
    files[fn] = string(text)
    handle(err, "Reading " + fn)
  }
  ext := strings.Split(fn, ".")[len(strings.Split(fn, ".")) - 1]
  switch ext{
  case "js": w.Header().Add("content-type", "application/javascript")
  case "css": w.Header().Add("content-type", "text/css")
  }
  fmt.Fprintf(w, "%s", files[fn])
}

var tasks map[string]bool
const startedQuery = "select * from records where job=? and stop=-1;"
func checkStarted(task string, now int64) bool {
  var ret bool
  if state, ok := tasks[task]; ok {
    ret = state
  }else{
    q, err := db.Prepare(startedQuery)
    handle(err, "Checking if " + task + " is already running")
    var tmp string
    err = q.QueryRow(task).Scan(&tmp)
    switch {
    case err == sql.ErrNoRows:
      ret = false
    case err != nil:
      ret = true
    }
    tasks[task] = ret
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

func runTxn(statement string, vals []string){
  txn, err := db.Begin()
  handle(err, "Creating a transaction")
  stmt, err := txn.Prepare(statement)
  handle(err, "Preparing statement")
  defer stmt.Close()
  _, err = stmt.Exec(makeInterfaces(vals)...)
  handle(err, "Executing statement")
  txn.Commit()
}

func parse(r *http.Request) (string, url.Values){
  return strings.Join(strings.Split(r.URL.Path, "/")[2:], "/"), r.URL.Query()
}

const startStatement = "insert into records(job, start, stop, params) values(?, ?, ?, ?)"
func start(w http.ResponseWriter, r *http.Request){
  task, params := parse(r)

  textParams, err := json.Marshal(params)
  handle(err, "Parsing params into json")

  now := time.Now().UTC().UnixNano()

  if checkStarted(task, now){
    w.WriteHeader(400)
    fmt.Fprintf(w, "%s", "Error: " + task + " already started.")
  }else{
    runTxn(startStatement, []string{task, strconv.FormatInt(now, 10), "-1", string(textParams)})
    w.WriteHeader(200)
    tasks[task] = true;
  }
}

const stopStatement = "update records set stop=?, params=params||'|'||? where job=? and stop=-1;"
func stop(w http.ResponseWriter, r *http.Request){
  task, params := parse(r)

  textParams, err := json.Marshal(params)
  handle(err, "Parsing params into json")

  now := time.Now().UTC().UnixNano()
  if checkStarted(task, now){
    runTxn(stopStatement, []string{strconv.FormatInt(now, 10), string(textParams), task})
    w.WriteHeader(200)
    tasks[task] = false
  }else{
    w.WriteHeader(400)
    fmt.Fprintf(w, "%s", "Error: " + task + " not running.")
  }
  go updateJobStats(task)
}

const updateJobStatement = "replace into jobs select job, avg(stop-start), stdev(stop-start), median(stop-start) from records where stop != -1 and job=?;"
func updateJobStats(task string){
  txn, err := db.Begin()
  handle(err, "Creating a transaction")
  stmt, err := txn.Prepare(statement)
  handle(err, "Preparing statement")
  defer stmt.Close()
  _, err = stmt.Exec(makeInterfaces(vals)...)
  handle(err, "Executing statement")
  txn.Commit()
  runTxn(updateJobStatement, []string{task})
}

func runQuery(query string, vals []string) *sql.Rows {
  rows, err := db.Query(query, makeInterfaces(vals)...)
  handle(err, "Running query" + query + " for " + strings.Join(vals, ","))
  return rows
}

const statusQuery = `
    select jobs.job, start, average, params from records
    join jobs on records.job = jobs.job
    where stop =-1;
  `
type Job struct{
  Start int64
  Average float64
  Params string
}
func status(w http.ResponseWriter, r *http.Request){
  jobs := make(map[string]Job)

  rows := runQuery(statusQuery, []string{})
  for rows.Next(){
    var key string
    job := Job{}
    rows.Scan(&key, &job.Start, &job.Average, &job.Params)
    jobs[key] = job
  }
  err := rows.Err()
  handle(err, "Iterating over status query results")

  text, err := json.Marshal(jobs)
  handle(err, "Marshalling jobs into json")
  fmt.Fprintf(w, "%s", string(text))
}

type Record struct{
  Job string
  Start int64
  Stop int64
  Params string
}
const jobHistoryQuery = "select * from records where job like '?%';"
func history(w http.ResponseWriter, r *http.Request){
  task, _ := parse(r)
  rows := runQuery(strings.Replace(jobHistoryQuery, "?", task, -1), []string{})
  records := []Record{}
  for rows.Next(){
    record := Record{}
    rows.Scan(&record.Job, &record.Start, &record.Stop, &record.Params)
    records = append(records, record)
  }

  text, err := json.Marshal(records)
  handle(err, "Marshalling records into json")
  fmt.Fprintf(w, "%s", string(text))
}

var db *sql.DB
const recordsDDL = "create table if not exists records (job text not null, start integer, stop integer, params string);"
const jobsDDL = "create table if not exists jobs (job text not null primary key, average real, stddev real, median real, params string);"
const jobsIndex = "create unique index if not exists jobsIndex on jobs(job);"
var DDLs = []string{recordsDDL, jobsDDL, jobsIndex}
func init(){
  var err error
  sql.Register("sqlite3_with_extensions",
		&sqlite3.SQLiteDriver{Extensions: []string{"./extension-functions.o"}})
  db, err = sql.Open("sqlite3_with_extensions", "./jobs.db")
  handle(err, "Opening connection to sqlite")

  for _, ddl := range DDLs{
    _, err = db.Exec(ddl)
    handle(err, "Executing DDL: " + ddl)
  }

  tasks = make(map[string]bool)
  files = make(map[string]string)
}

func main() {
  //static files
  http.HandleFunc("/www/", static)
  http.HandleFunc("/bower_components/", static)

  //data services
  http.HandleFunc("/start/", start)
  http.HandleFunc("/stop/", stop)
  http.HandleFunc("/history/", history)
  http.HandleFunc("/status", status)

  log.Print("Starting..")
  http.ListenAndServe(":8080", nil)
}
