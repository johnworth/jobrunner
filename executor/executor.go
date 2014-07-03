package executor

import (
	"fmt"
	"path"

	"github.com/johnworth/jobrunner/config"
	"github.com/johnworth/jobrunner/jobs"
)

// JSONCmd represents a single command for a job as sent by a client.
type JSONCmd struct {
	CommandLine string
	Environment map[string]string
	WorkingDir  string
}

// StartMsg represents a job start request
type StartMsg struct {
	Commands   []JSONCmd
	WorkingDir string
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
func (r Registry) Register(j *jobs.Job) {
	uuid := j.UUID()
	r <- registryCommand{action: set, key: uuid, value: j}
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
func (r Registry) Delete(j *jobs.Job) {
	uuid := j.UUID()
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
func (e *Executor) Execute(msg *StartMsg) (string, []string, error) {
	cfg := config.Get()
	var commandIDs []string
	job := jobs.NewJob()

	//Set the working directory for the job.
	var workingDir string
	if msg.WorkingDir == "" {
		//default working directory
		workingDir = path.Join(cfg.BaseDir, job.UUID())
	} else {
		//user specified working directory
		workingDir = path.Join(cfg.BaseDir, msg.WorkingDir)
	}
	job.SetWorkingDir(workingDir)

	//Make sure the job is prepared. In other words, create the working dir.
	err := job.Prepare()
	if err != nil {
		return "", nil, err
	}

	//Add commands to the job
	for _, c := range msg.Commands {
		bash := jobs.NewBashCommand()
		if c.WorkingDir != "" {
			bash.SetWorkingDir(path.Join(workingDir, c.WorkingDir))
		} else {
			bash.SetWorkingDir(workingDir)
		}
		err = bash.Prepare(c.CommandLine, c.Environment)
		if err != nil {
			return "", nil, err
		}
		job.AddCommand(bash)
		commandIDs = append(commandIDs, bash.UUID())
	}
	e.Registry.Register(job)
	go job.Run()
	return job.UUID(), commandIDs, err
}

// Kill terminates the specified job with extreme prejudice.
func (e *Executor) Kill(job *jobs.Job) {
	job.Kill()
}

// Clean tells executor to clean up the job. The job does not have to be running
// and it might actually be better if it's done before you clean it up.
func (e *Executor) Clean(job *jobs.Job) error {
	if !job.Completed() {
		return fmt.Errorf("Job %s must be completed or killed before it can be cleaned.", job.UUID())
	}
	if e.Registry.HasKey(job.UUID()) {
		e.Registry.Delete(job)
	}
	err := job.Clean()
	return err
}
