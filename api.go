package main

import (
	"encoding/json"
	"expvar"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"runtime"

	"github.com/gorilla/mux"
)

func addrsAsStrings() interface{} {
	var retval []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		retval = []string{"Couldn't get interface addresses."}
	} else {
		retval = make([]string, 0)
		for _, addr := range addrs {
			retval = append(retval, addr.String())
		}
	}
	return retval
}

func init() {
	cpus := expvar.NewInt("numcpus")
	cpus.Set(int64(runtime.NumCPU()))

	goversion := expvar.NewString("goversion")
	goversion.Set(runtime.Version())

	expvar.Publish("addrs", expvar.Func(addrsAsStrings))
}

// StartJobMsg represents a job start request
type StartJobMsg struct {
	CommandLine string
	Environment map[string]string
}

// JobIDMsg represents a JobID response
type JobIDMsg struct {
	JobID string
}

// APIHandlers defines handlers for the endpoints and gives them access to a
// JobExecutor.
type APIHandlers struct {
	Executor *JobExecutor
}

// NewAPIHandlers constructs a new instance of APIHandlers and returns a pointer
// to it.
func NewAPIHandlers() *APIHandlers {
	return &APIHandlers{
		Executor: NewJobExecutor(),
	}
}

//StartJob starts a job and returns a job ID.
func (h *APIHandlers) StartJob(resp http.ResponseWriter, r *http.Request) {
	dec := json.NewDecoder(r.Body)
	var jobMsg StartJobMsg
	if err := dec.Decode(&jobMsg); err != nil {
		http.Error(resp, err.Error(), 500)
		return
	}
	if jobMsg.CommandLine == "" {
		http.Error(resp, "Missing CommandLine key.", 500)
		return
	}
	if jobMsg.Environment == nil {
		http.Error(resp, "Missing Environment key.", 500)
		return
	}
	jid := h.Executor.Launch(jobMsg.CommandLine, jobMsg.Environment)
	returnMsg, err := json.Marshal(&JobIDMsg{
		JobID: jid,
	})
	if err != nil {
		http.Error(resp, err.Error(), 500)
		return
	}

	fmt.Fprintf(resp, string(returnMsg[:]))
}

// GetJobInfo returns info about a running job.
func (h *APIHandlers) GetJobInfo(resp http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fmt.Fprintf(resp, "getJobInfo: %s\n", vars["jobID"])
}

// ListJobsResponse represents a the return value for the ListJobs endpoint.
type ListJobsResponse struct {
	JobIDs []string
}

// ListJobs returns a list of all of the running jobs.
func (h *APIHandlers) ListJobs(resp http.ResponseWriter, r *http.Request) {
	jobs, err := json.Marshal(&ListJobsResponse{
		JobIDs: h.Executor.Registry.ListJobs(),
	})
	if err != nil {
		http.Error(resp, err.Error(), 500)
		return
	}
	fmt.Fprintf(resp, string(jobs[:]))
}

// AttachToJob streams a combined stdout/stderr for a running job
// as a chunked response.
func (h *APIHandlers) AttachToJob(resp http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	runID := vars["runID"]
	jobRegistry := h.Executor.Registry
	if !jobRegistry.HasKey(runID) {
		http.Error(resp, fmt.Sprintf("Job %s not found.", runID), 404)
		return
	}
	syncer := jobRegistry.Get(runID)
	outputListener := syncer.OutputRegistry.AddListener()
	reader := NewJobOutputReader(outputListener)
	defer reader.Quit()
	defer syncer.OutputRegistry.RemoveListener(outputListener)
	io.Copy(resp, reader)
}

// KillJob kills a running job.
func (h *APIHandlers) KillJob(resp http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	runID := vars["runID"]
	h.Executor.Kill(runID)
}

// SetupRouter uses Gorilla's mux project to set up a router and returns it.
func (h *APIHandlers) SetupRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/{jobID}", h.GetJobInfo).Methods("GET")
	r.HandleFunc("/", h.StartJob).Methods("POST")
	r.HandleFunc("/", h.ListJobs).Methods("GET")
	r.HandleFunc("/{runID}/attach", h.AttachToJob).Methods("GET")
	r.HandleFunc("/{runID}", h.KillJob).Methods("DELETE")
	return r
}

// launchExpvarServer starts up a goroutine for handling expvar requests.
func (h *APIHandlers) launchExpvarServer(port string) {
	go func() {
		expserver := &http.Server{
			Addr: port,
		}
		log.Fatal(expserver.ListenAndServe())
	}()
}

// NewServer calls setupRouter(), constructs a server and fires it up.
func (h *APIHandlers) NewServer() *http.Server {
	h.launchExpvarServer(":8081")
	m := h.SetupRouter()
	s := &http.Server{
		Addr:    ":8080",
		Handler: m,
	}
	log.Println("Returning server")
	return s
}
