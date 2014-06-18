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

// StartMsg represents a job start request
type StartMsg struct {
	CommandLine string
	Environment map[string]string
}

// IDMsg represents a ID response
type IDMsg struct {
	ID string
}

// APIHandlers defines handlers for the endpoints and gives them access to a
// Executor.
type APIHandlers struct {
	Executor *Executor
}

// NewAPIHandlers constructs a new instance of APIHandlers and returns a pointer
// to it.
func NewAPIHandlers() *APIHandlers {
	return &APIHandlers{
		Executor: NewExecutor(),
	}
}

//Start starts a job and returns a job ID.
func (h *APIHandlers) Start(resp http.ResponseWriter, r *http.Request) {
	dec := json.NewDecoder(r.Body)
	var jobMsg StartMsg
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
	returnMsg, err := json.Marshal(&IDMsg{
		ID: jid,
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
	syncer := jobRegistry.Get(ID)
	outputListener := syncer.OutputRegistry.AddListener()
	reader := NewOutputReader(outputListener)
	defer reader.Quit()
	defer syncer.OutputRegistry.RemoveListener(outputListener)
	io.Copy(resp, reader)
}

// Kill kills a running job.
func (h *APIHandlers) Kill(resp http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	ID := vars["ID"]
	h.Executor.Kill(ID)
}

// SetupRouter uses Gorilla's mux project to set up a router and returns it.
func (h *APIHandlers) SetupRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/{ID}", h.GetInfo).Methods("GET")
	r.HandleFunc("/", h.Start).Methods("POST")
	r.HandleFunc("/", h.List).Methods("GET")
	r.HandleFunc("/{ID}/attach", h.Attach).Methods("GET")
	r.HandleFunc("/{ID}", h.Kill).Methods("DELETE")
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
