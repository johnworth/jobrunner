package api

import (
	"encoding/json"
	"expvar"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime"

	"github.com/gorilla/mux"
	"github.com/johnworth/jobrunner/config"
	"github.com/johnworth/jobrunner/executor"
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

// APIHandlers defines handlers for the endpoints and gives them access to a
// Executor.
type APIHandlers struct {
	Executor *executor.Executor
}

// NewAPIHandlers constructs a new instance of APIHandlers and returns a pointer
// to it.
func NewAPIHandlers() *APIHandlers {
	return &APIHandlers{
		Executor: executor.NewExecutor(),
	}
}

//Start starts a job and returns a job ID.
func (h *APIHandlers) Start(resp http.ResponseWriter, r *http.Request) {
	dec := json.NewDecoder(r.Body)
	var jobMsg executor.StartMsg
	if err := dec.Decode(&jobMsg); err != nil {
		http.Error(resp, err.Error(), 500)
		return
	}
	if jobMsg.Commands == nil {
		http.Error(resp, "Missing JSONCommands key.", 500)
		return
	}
	for _, j := range jobMsg.Commands {
		if j.CommandLine == "" {
			http.Error(resp, "Missing CommandLine from command.", 500)
			return
		}
		if j.Environment == nil {
			http.Error(resp, "Missing Environment from command.", 500)
			return
		}
	}
	jid, cids, err := h.Executor.Execute(&jobMsg)
	if err != nil {
		http.Error(resp, err.Error(), 500)
		return
	}
	returnMsg, err := json.Marshal(&executor.IDMsg{
		JobID:      jid,
		CommandIDs: cids,
	})
	if err != nil {
		http.Error(resp, err.Error(), 500)
		return
	}

	fmt.Fprintf(resp, string(returnMsg[:]))
}

// GetInfo returns info about a running job.
func (h *APIHandlers) GetInfo(resp http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fmt.Fprintf(resp, "GetInfo: %s\n", vars["ID"])
}

// ListResponse represents a the return value for the List endpoint.
type ListResponse struct {
	IDs []string
}

// List returns a list of all of the running jobs.
func (h *APIHandlers) List(resp http.ResponseWriter, r *http.Request) {
	jobs, err := json.Marshal(&ListResponse{
		IDs: h.Executor.Registry.List(),
	})
	if err != nil {
		http.Error(resp, err.Error(), 500)
		return
	}
	fmt.Fprintf(resp, string(jobs[:]))
}

// Attach streams a combined stdout/stderr for a running job
// as a chunked response.
func (h *APIHandlers) Attach(resp http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	ID := vars["ID"]
	jobRegistry := h.Executor.Registry
	if !jobRegistry.HasKey(ID) {
		http.Error(resp, fmt.Sprintf("Job %s not found.", ID), 404)
		return
	}
	//job := jobRegistry.Get(ID)
	fmt.Fprintf(resp, ID)
}

// Kill kills a running job.
func (h *APIHandlers) Kill(resp http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	ID := vars["ID"]
	if h.Executor.Registry.HasKey(ID) {
		job := h.Executor.Registry.Get(ID)
		if !job.Completed() {
			h.Executor.Kill(job)
		}
	}
}

//Clean triggers a clean up of disk resources used by the job.
func (h *APIHandlers) Clean(resp http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	ID := vars["ID"]
	if !h.Executor.Registry.HasKey(ID) {
		http.Error(resp, fmt.Sprintf("Job %s not found", ID), 404)
		return
	}
	job := h.Executor.Registry.Get(ID)
	h.Executor.Clean(job)
}

// SetupRouter uses Gorilla's mux project to set up a router and returns it.
func (h *APIHandlers) SetupRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/{ID}", h.GetInfo).Methods("GET")
	r.HandleFunc("/", h.Start).Methods("POST")
	r.HandleFunc("/", h.List).Methods("GET")
	r.HandleFunc("/{ID}/clean", h.Clean).Methods("POST")
	r.HandleFunc("/{ID}", h.Kill).Methods("DELETE")
	return r
}

// NewServer calls setupRouter(), constructs a server and fires it up.
func (h *APIHandlers) NewServer() *http.Server {
	conf := config.Get()
	m := h.SetupRouter()
	http.Handle("/", m)
	s := &http.Server{
		Addr: conf.Host,
	}
	log.Println("Returning server")
	return s
}
