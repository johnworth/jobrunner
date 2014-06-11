package main

import (
	"fmt"
	"log"
	"os/exec"
	"syscall"
)

// exitCode returns the integer exit code of a command. Start() and Wait()
// should have already been called on the Command.
func exitCode(cmd *exec.Cmd) int {
	return cmd.ProcessState.Sys().(syscall.WaitStatus).ExitStatus()
}

// JobOutputListener is the message struct for the Setter and Remove channels
// in a JobOutputRegistry instance. The Listener is the channel that job outputs
// are sent out on. The Latch channel is used for synchronizing the
// AddListener() and RemoveListener() calls.
type JobOutputListener struct {
	Listener chan []byte
	Latch    chan int
}

// JobOutputRegistry contains a list of channels that accept []byte's. Each job
// gets its own JobOutputRegistry. The JobOutputRegistry is referred to inside
// the job's JobSyncer instance, which in turn is referred to within the the
// JobRegistry.
type JobOutputRegistry struct {
	Input    chan []byte
	Setter   chan *JobOutputListener
	Remove   chan *JobOutputListener
	Registry map[*JobOutputListener]chan []byte
}

// NewJobOutputRegistry returns a pointer to a new instance of JobOutputRegistry.
func NewJobOutputRegistry() *JobOutputRegistry {
	return &JobOutputRegistry{
		Input:    make(chan []byte),
		Setter:   make(chan *JobOutputListener),
		Remove:   make(chan *JobOutputListener),
		Registry: make(map[*JobOutputListener]chan []byte),
	}
}

// Listen fires off a goroutine that can be communicated with through the Input,
// Setter, and Remove channels.
func (j *JobOutputRegistry) Listen() {
	go func() {
		for {
			select {
			case a := <-j.Setter:
				j.Registry[a] = a.Listener
				a.Latch <- 1
			case buf := <-j.Input:
				for _, ch := range j.Registry {
					ch <- buf
				}
			case rem := <-j.Remove:
				delete(j.Registry, rem)
				rem.Latch <- 1
			}
		}
	}()
}

// AddListener creates a JobOutputListener, adds it to the JobOutputRegistry,
// and returns a pointer to the JobOutputListener. Synchronizes with the
// JobOutputRegistry goroutine through the JobOutputListener's Latch channel.
func (j *JobOutputRegistry) AddListener() *JobOutputListener {
	listener := make(chan []byte)
	latch := make(chan int)
	adder := &JobOutputListener{
		Listener: listener,
		Latch:    latch,
	}
	j.Setter <- adder
	<-latch
	return adder
}

// RemoveListener removes the passed in JobOutputListener from the
// JobOutputRegistry. Synchronizes with the JobOuputRegistry goroutine through
// the JobOutputListener's Latch channel. Does not close any channels.
func (j *JobOutputRegistry) RemoveListener(l *JobOutputListener) {
	j.Remove <- l
	<-l.Latch
}

// JobSyncer contains channels that can be used to communicate with a job
// goroutine. It also contains a pointer to a JobOutputRegistry.
type JobSyncer struct {
	Command        chan string
	Environment    chan map[string]string
	Start          chan int
	Output         chan []byte
	ExitCode       chan int
	OutputRegistry *JobOutputRegistry
}

// NewJobSyncer creates a new instance of JobSyncer and returns a pointer to it.
func NewJobSyncer() *JobSyncer {
	s := &JobSyncer{
		Command:        make(chan string),
		Environment:    make(chan map[string]string),
		Start:          make(chan int),
		Output:         make(chan []byte),
		ExitCode:       make(chan int),
		OutputRegistry: NewJobOutputRegistry(),
	}
	s.OutputRegistry.Listen()
	return s
}

// Write sends the []byte array passed out on RoutineWriter's OutChannel
func (j *JobSyncer) Write(p []byte) (n int, err error) {
	fmt.Println(string(p[:]))
	j.OutputRegistry.Input <- p
	return len(p), nil
}

// JobRegistryGetMsg wraps a key that you want to retrieve from a JobRegistry
// and a Resp channel that the retrieved value should be sent back on.
type JobRegistryGetMsg struct {
	Key  string
	Resp chan<- *JobSyncer
}

// JobRegistrySetMsg wraps a key that you want to set, the Value (a *JobSyncer)
// that should be associated with the key, and a Latch channel that will be
// sent an integer when the Value has been set.
type JobRegistrySetMsg struct {
	Key   string
	Value *JobSyncer
	Latch chan int
}

// JobRegistry encapsulates a map associating a string with a *JobSyncer. This
// will let additional goroutines communicate with the goroutine running the
// job associated with the key. The key will most likely be a UUID. There should
// only be one instance on JobRegistry per jobrunner instance.
type JobRegistry struct {
	Setter   chan *JobRegistrySetMsg
	Getter   chan *JobRegistryGetMsg
	Registry map[string]*JobSyncer
}

// NewJobRegistry creates a new instance of JobRegistry and returns a pointer to
// it. Does not make sure that only one instance is created.
func NewJobRegistry() *JobRegistry {
	return &JobRegistry{
		Setter:   make(chan *JobRegistrySetMsg),
		Getter:   make(chan *JobRegistryGetMsg),
		Registry: make(map[string]*JobSyncer),
	}
}

// Listen launches a goroutine that can be communicated with via the setter
// and getter channels passed in. Listen is non-blocking.
func (j *JobRegistry) Listen() {
	go func() {
		for {
			select {
			case s := <-j.Setter:
				j.Registry[s.Key] = s.Value
				s.Latch <- 1
			case g := <-j.Getter:
				g.Resp <- j.Registry[g.Key]
			}
		}
	}()
}

// RegisterJobSyncer adds the *JobSyncer to the registry with the key set to the
// value of uuid.
func (j *JobRegistry) RegisterJobSyncer(uuid string, s *JobSyncer) {
	m := &JobRegistrySetMsg{
		Key:   uuid,
		Value: s,
		Latch: make(chan int),
	}
	j.Setter <- m
	<-m.Latch
}

// JobExecutor maintains a reference to a JobRegistry and is able to launch
// jobs. There should only be one instance of JobExecutor per instance of
// jobrunner, but there isn't anything to prevent you from creating more than
// one.
type JobExecutor struct {
	Registry *JobRegistry
}

// NewJobExecutor creates a new instance of JobExecutor and returns a pointer to
// it.
func NewJobExecutor() *JobExecutor {
	e := &JobExecutor{
		Registry: NewJobRegistry(),
	}
	e.Registry.Listen()
	return e
}

// Launch fires off a new job, adding its JobSyncer instance to the job registry.
func (j *JobExecutor) Launch(command string, environment map[string]string) string {
	syncer := NewJobSyncer()
	jobID := "foo"
	j.Registry.RegisterJobSyncer(jobID, syncer)

	//Set up the output listener
	j.Execute(syncer)
	syncer.Command <- command
	syncer.Environment <- environment
	syncer.Start <- 1
	return jobID
}

// Execute starts up a goroutine that communicates via a JobSyncer and will
// eventually execute a job.
func (j *JobExecutor) Execute(s *JobSyncer) {
	go func() {
		cmdString := <-s.Command
		environment := <-s.Environment
		log.Println(environment)
		cmd := exec.Command("bash", "-c", cmdString)
		cmd.Env = formatEnv(environment)
		fmt.Println(cmd.Env)
		cmd.Stdout = s
		cmd.Stderr = s
		<-s.Start
		fmt.Println("Starting")
		err := cmd.Start()
		if err != nil {
			fmt.Println(err)
			s.ExitCode <- -1000
			return
		}
		err = cmd.Wait()
		if err != nil {
			fmt.Println(err)
			s.ExitCode <- -2000
			return
		}
		s.ExitCode <- exitCode(cmd)
	}()
}

func formatEnv(env map[string]string) []string {
	output := make([]string, 1)
	for key, val := range env {
		output = append(output, fmt.Sprintf("%s=%s", key, val))
	}
	return output
}
