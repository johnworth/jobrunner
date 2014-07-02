package executor

import (
	"log"

	"github.com/johnworth/jobrunner/jobs"

	"code.google.com/p/go-uuid/uuid"
)

// JSONCmd represents a single command for a job as sent by a client.
type JSONCmd struct {
	CommandLine string
	Environment map[string]string
	WorkingDir  string
}

// StartMsg represents a job start request
type StartMsg struct {
	Commands []JSONCmd
}

// IDMsg represents a ID response
type IDMsg struct {
	JobID      string
	CommandIDs []string
}

type registryCommand struct {
	action registryAction
	key    string
	value  interface{}
	result chan<- interface{}
}

type registryAction int

const (
	remove registryAction = iota
	find
	set
	get
	length
	quit
	listkeys
)

// Registry maintains a map of UUIDs associated with Job instances.
type Registry chan registryCommand

// NewRegistry returns a new instance of Registry.
func NewRegistry() Registry {
	r := make(Registry)
	go r.run()
	return r
}

type registryFindResult struct {
	found  bool
	result *jobs.Job
}

// run launches a goroutine that can be communicated with by the Registry
// channel.
func (r Registry) run() {
	reg := make(map[string]*jobs.Job)
	for command := range r {
		switch command.action {
		case set:
			reg[command.key] = command.value.(*jobs.Job)
		case get:
			val, found := reg[command.key]
			command.result <- registryFindResult{found, val}
		case length:
			command.result <- len(reg)
		case remove:
			delete(reg, command.key)
		case listkeys:
			var retval []string
			for k := range reg {
				retval = append(retval, k)
			}
			command.result <- retval
		case quit:
			close(r)
		}
	}
}

// Register associates 'uuid' with a *Job in the registry.
func (r Registry) Register(uuid string, s *jobs.Job) {
	r <- registryCommand{action: set, key: uuid, value: s}
}

// Get returns the *Job for the given uuid in the registry.
func (r Registry) Get(uuid string) *jobs.Job {
	reply := make(chan interface{})
	regCmd := registryCommand{action: get, key: uuid, result: reply}
	r <- regCmd
	result := (<-reply).(registryFindResult)
	return result.result
}

// HasKey returns true if a job associated with uuid in the registry.
func (r Registry) HasKey(uuid string) bool {
	reply := make(chan interface{})
	regCmd := registryCommand{action: get, key: uuid, result: reply}
	r <- regCmd
	result := (<-reply).(registryFindResult)
	return result.found
}

// List returns the list of jobs in the registry.
func (r Registry) List() []string {
	reply := make(chan interface{})
	regCmd := registryCommand{action: listkeys, result: reply}
	r <- regCmd
	result := (<-reply).([]string)
	return result
}

// Delete deletes a *Job from the registry.
func (r Registry) Delete(uuid string) {
	r <- registryCommand{action: remove, key: uuid}
}

// Executor maintains a reference to a Registry and is able to launch
// jobs. There should only be one instance of Executor per instance of
// jobrunner, but there isn't anything to prevent you from creating more than
// one.
type Executor struct {
	Registry Registry
}

// NewExecutor returns a pointer to a newly created Executor.
func NewExecutor() *Executor {
	e := &Executor{
		Registry: NewRegistry(),
	}
	return e
}

// Execute processes the JSONCmds passed in a Runs a job.
func (e *Executor) Execute(cmds *[]JSONCmd) (string, []string) {
	var commandIDs []string
	jobID := uuid.New()
	job := jobs.NewJob()
	job.SetUUID(jobID)
	e.Registry.Register(jobID, job)
	log.Printf("Registering job %s.", jobID)
	for _, c := range *cmds {
		cid := uuid.New()
		bash := jobs.NewBashCommand()
		bash.SetUUID(cid)
		bash.Prepare(c.CommandLine, c.Environment)
		job.AddCommand(bash)
		commandIDs = append(commandIDs, cid)
	}
	go func(jid string) {
		job.Run()
		e.Registry.Delete(jid)
	}(jobID)
	return jobID, commandIDs
}

// Kill terminates the specified job with extreme prejudice.
func (e *Executor) Kill(uuid string) {
	if e.Registry.HasKey(uuid) {
		job := e.Registry.Get(uuid)
		job.Kill()
		e.Registry.Delete(uuid)
	}
}
