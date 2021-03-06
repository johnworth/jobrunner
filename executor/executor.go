package executor

import (
	"fmt"
	"log"
	"path"

	"github.com/fsouza/go-dockerclient"
	"github.com/johnworth/jobrunner/config"
	"github.com/johnworth/jobrunner/jobs"
	"github.com/johnworth/jobrunner/jsonify"
)

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
			//golint shows a warning about this, but it's wrong for what we need.
			//if you do 'var retval []string' the empty list shows up as null in
			//the listing endpoint JSON. if you do this, you get an empty list.
			retval := make([]string, 0)
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
	Docker   *docker.Client
}

// NewExecutor returns a pointer to a newly created Executor.
func NewExecutor() (*Executor, error) {
	cfg := config.Get()
	dockercl, err := docker.NewClient(cfg.DockerString)
	if err != nil {
		return nil, err
	}
	e := &Executor{
		Registry: NewRegistry(),
		Docker:   dockercl,
	}
	return e, err
}

// Execute processes the JSONCmds passed in a Runs a job.
func (e *Executor) Execute(msg *jsonify.StartMsg) (string, []string, error) {
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
	log.Printf("Working directory for %s is %s\n", job.UUID(), workingDir)

	//Make sure the job is prepared. In other words, create the working dir.
	err := job.Prepare()
	if err != nil {
		return "", nil, err
	}

	//Add commands to the job
	for _, c := range msg.Commands {
		var command jobs.JobCommand
		var UUID string
		switch c.Kind {
		case "bash":
			b := jobs.NewBashCommand(c)
			UUID = b.UUID()
			command = b
		case "docker":
			d := jobs.NewDockerCommand(e.Docker, c)
			UUID = d.UUID
			command = d
		default:
			return "", nil, fmt.Errorf("Unknown command type '%s'", c.Kind)
		}
		if err != nil {
			return "", nil, err
		}
		job.AddCommand(command)
		commandIDs = append(commandIDs, UUID)
		fmt.Printf("Added command %s to job %s\n", UUID, job.UUID())
		fmt.Printf("Command line for %s is %s\n", UUID, c.CommandLine)
	}
	for _, c := range job.Commands() {
		fmt.Println(c)
	}
	e.Registry.Register(job)
	go func(job *jobs.Job) {
		for _, cmd := range job.Commands() {
			err = cmd.Setup(job.WorkingDir())
			if err != nil {
				log.Println(err.Error())
			}
		}
		job.Run()
	}(job)
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
