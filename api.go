package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

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

// ListJobs returns a list of all of the running jobs.
func (h *APIHandlers) ListJobs(resp http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(resp, "listJobs\n")
}

// AttachToJob streams a combined stdout/stderr for a running job
// as a chunked response.
func (h *APIHandlers) AttachToJob(resp http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	runID := vars["runID"]
	syncer := h.Executor.Registry.Get(runID)
	outputListener := syncer.OutputRegistry.AddListener()
	reader := NewJobOutputReader(outputListener)
	defer reader.Quit()
	defer syncer.OutputRegistry.RemoveListener(outputListener)
	io.Copy(resp, reader)
}

// SetupRouter uses Gorilla's mux project to set up a router and returns it.
func (h *APIHandlers) SetupRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/{jobID}", h.GetJobInfo).Methods("GET")
	r.HandleFunc("/", h.StartJob).Methods("POST")
	r.HandleFunc("/", h.ListJobs).Methods("GET")
	r.HandleFunc("/{runID}/attach", h.AttachToJob).Methods("GET")
	return r
}

// NewServer calls setupRouter(), constructs a server and fires it up.
func (h *APIHandlers) NewServer() *http.Server {
	m := h.SetupRouter()
	s := &http.Server{
		Addr:    ":8080",
		Handler: m,
	}
	log.Println("Returning server")
	return s
}
