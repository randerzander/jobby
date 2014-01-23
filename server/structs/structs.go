package structs

type RunningJob struct{
  Job string
  Start int64
  Average float64
}

type JobRecord struct{
  Job string
  Start, Stop int64
}

type Statement struct {
  Statement string
  Args []string
}
