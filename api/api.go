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
	"github.com/johnworth/jobrunner/jsonify"
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
func NewAPIHandlers() (*APIHandlers, error) {
	e, err := executor.NewExecutor()
	if err != nil {
		return nil, err
	}
	handlers := &APIHandlers{e}
	return handlers, err
}

func validateMsgCommand(resp http.ResponseWriter, j *jsonify.JSONCmd) bool {
	validKinds := map[string]bool{
		"bash":   true,
		"docker": true,
	}
	if j.Kind == "" {
		http.Error(resp, "Missing Kind from command.", 500)
		return false
	}
	if !validKinds[j.Kind] {
		http.Error(resp, fmt.Sprintf("Missing invalid kind of command: %s", j.Kind), 500)
		return false
	}
	if j.CommandLine == "" {
		http.Error(resp, "Missing CommandLine from command.", 500)
		return false
	}
	if j.Environment == nil {
		http.Error(resp, "Missing Environment from command.", 500)
		return false
	}
	if j.Kind == "docker" {
		if j.Repository == "" {
			http.Error(resp, "Missing docker repository from command.", 800)
			return false
		}
	}
	return true
}

//Start starts a job and returns a job ID.
func (h *APIHandlers) Start(resp http.ResponseWriter, r *http.Request) {
	dec := json.NewDecoder(r.Body)
	var jobMsg jsonify.StartMsg
	if err := dec.Decode(&jobMsg); err != nil {
		http.Error(resp, err.Error(), 500)
		return
	}
	if jobMsg.Commands == nil {
		http.Error(resp, "Missing JSONCommands key.", 500)
		return
	}
	for _, j := range jobMsg.Commands {
		isValid := validateMsgCommand(resp, &j)
		if !isValid {
			return
		}
	}
	jid, cids, err := h.Executor.Execute(&jobMsg)
	if err != nil {
		http.Error(resp, err.Error(), 500)
		return
	}
	returnMsg, err := json.Marshal(&jsonify.IDMsg{
		JobID:      jid,
		CommandIDs: cids,
	})
	if err != nil {
		http.Error(resp, err.Error(), 500)
		return
	}

	fmt.Fprintf(resp, string(returnMsg[:]))
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
