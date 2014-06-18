package main

import (
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"

	"code.google.com/p/go-uuid/uuid"
)

// exitCode returns the integer exit code of a command. Start() and Wait()
// should have already been called on the Command.
func exitCode(cmd *exec.Cmd) int {
	return cmd.ProcessState.Sys().(syscall.WaitStatus).ExitStatus()
}

// JobOutputQuitMsg is sent to the JobOutputRegistry on the Quit channel when
// a job completes. The JobOutputRegistry then tells all of the registered
// JobOutputListeners to quit.
type JobOutputQuitMsg struct {
	Latch chan int
}

// JobOutputListener is the message struct for the Setter and Remove channels
// in a JobOutputRegistry instance. The Listener is the channel that job outputs
// are sent out on. The Latch channel is used for synchronizing the
// AddListener() and RemoveListener() calls.
type JobOutputListener struct {
	Listener   chan []byte
	Latch      chan int
	Quit       chan int
	readBuffer []byte
}

// NewJobOutputListener returns a new instance of JobOutputListener.
func NewJobOutputListener() *JobOutputListener {
	return &JobOutputListener{
		Listener:   make(chan []byte),
		Latch:      make(chan int),
		Quit:       make(chan int),
		readBuffer: make([]byte, 0),
	}
}

// JobOutputReader will buffer and allow Read()s from data sent via a
// JobOutputListener
type JobOutputReader struct {
	accum       []byte
	listener    *JobOutputListener
	m           *sync.Mutex
	QuitChannel chan int
	EOF         bool
}

// NewJobOutputReader will create a new JobOutputReader with the given
// JobOutputListener.
func NewJobOutputReader(l *JobOutputListener) *JobOutputReader {
	r := &JobOutputReader{
		accum:       make([]byte, 0),
		listener:    l,
		m:           &sync.Mutex{},
		QuitChannel: make(chan int),
		EOF:         false,
	}
	go func() {
		for {
			select {
			case inBytes := <-r.listener.Listener:
				r.m.Lock()
				for _, b := range inBytes {
					r.accum = append(r.accum, b)
				}
				r.m.Unlock()
			case <-r.listener.Quit: //Quit if the listener tells us to.
				r.m.Lock()
				r.EOF = true
				r.m.Unlock()
				break
			case <-r.QuitChannel: //Quit if a caller tells us to.
				r.m.Lock()
				r.EOF = true
				r.m.Unlock()
				break
			}
		}
	}()
	return r
}

// Reader will do a consuming read from the JobOutputReader's buffer. This makes
// it implement the Reader interface.
func (r *JobOutputReader) Read(p []byte) (n int, err error) {
	r.m.Lock()
	defer r.m.Unlock()
	if r.EOF && len(r.accum) == 0 {
		return 0, io.EOF
	}
	if len(r.accum) == 0 {
		return 0, nil
	}
	var bytesRead int
	if len(r.accum) <= len(p) {
		bytesRead = copy(p, r.accum)
		r.accum = make([]byte, 0)
		if r.EOF {
			return bytesRead, io.EOF
		}
		return bytesRead, nil
	}
	bytesRead = copy(p, r.accum)
	r.accum = r.accum[bytesRead:]
	if r.EOF && (len(r.accum) <= 0) {
		return bytesRead, io.EOF
	}
	return bytesRead, nil

}

// Quit will tell the goroutine that pushes data into the buffer to quit.
func (r *JobOutputReader) Quit() {
	r.m.Lock()
	r.EOF = true
	r.m.Unlock()
	r.QuitChannel <- 1
}

// JobOutputRegistry contains a list of channels that accept []byte's. Each job
// gets its own JobOutputRegistry. The JobOutputRegistry is referred to inside
// the job's JobSyncer instance, which in turn is referred to within the the
// JobRegistry.
type JobOutputRegistry struct {
	Input       chan []byte
	Setter      chan *JobOutputListener
	Remove      chan *JobOutputListener
	Registry    map[*JobOutputListener]chan []byte
	QuitChannel chan *JobOutputQuitMsg
}

// NewJobOutputRegistry returns a pointer to a new instance of JobOutputRegistry.
func NewJobOutputRegistry() *JobOutputRegistry {
	return &JobOutputRegistry{
		Input:       make(chan []byte),
		Setter:      make(chan *JobOutputListener),
		Remove:      make(chan *JobOutputListener),
		Registry:    make(map[*JobOutputListener]chan []byte),
		QuitChannel: make(chan *JobOutputQuitMsg),
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
			case q := <-j.QuitChannel:
				for l := range j.Registry {
					l.Quit <- 1
				}
				q.Latch <- 1
				break
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
		Listener:   listener,
		Latch:      latch,
		Quit:       make(chan int),
		readBuffer: make([]byte, 0),
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

// Quit tells the JobOutputRegistry's goroutine to exit.
func (j *JobOutputRegistry) Quit() {
	msg := &JobOutputQuitMsg{
		Latch: make(chan int),
	}
	j.QuitChannel <- msg
	<-msg.Latch
}

// JobSyncer contains channels that can be used to communicate with a job
// goroutine. It also contains a pointer to a JobOutputRegistry.
type JobSyncer struct {
	Command        chan string
	Environment    chan map[string]string
	Start          chan int
	Started        chan int
	Kill           chan int
	Killed         bool
	Output         chan []byte
	ExitCode       int
	Completed      chan int
	CmdPtr         *exec.Cmd
	OutputRegistry *JobOutputRegistry
}

// NewJobSyncer creates a new instance of JobSyncer and returns a pointer to it.
func NewJobSyncer() *JobSyncer {
	s := &JobSyncer{
		Command:        make(chan string),
		Environment:    make(chan map[string]string),
		Start:          make(chan int),
		Started:        make(chan int),
		Kill:           make(chan int),
		Killed:         false,
		Output:         make(chan []byte),
		ExitCode:       -9000,
		Completed:      make(chan int),
		CmdPtr:         nil,
		OutputRegistry: NewJobOutputRegistry(),
	}
	s.OutputRegistry.Listen()
	return s
}

// Write sends the []byte array passed out on RoutineWriter's OutChannel
func (j *JobSyncer) Write(p []byte) (n int, err error) {
	j.OutputRegistry.Input <- p
	return len(p), nil
}

// Quit tells the JobSyncer to clean up its OutputRegistry
func (j *JobSyncer) Quit() {
	j.OutputRegistry.Quit()
}

// JobRegistryGetMsg wraps a key that you want to retrieve from a JobRegistry
// and a Resp channel that the retrieved value should be sent back on.
type JobRegistryGetMsg struct {
	Key  string
	Resp chan *JobSyncer
}

// JobRegistrySetMsg wraps a key that you want to set, the Value (a *JobSyncer)
// that should be associated with the key, and a Latch channel that will be
// sent an integer when the Value has been set.
type JobRegistrySetMsg struct {
	Key   string
	Value *JobSyncer
	Latch chan int
}

// JobRegistryListMsg is the struct that job UUIDs are returned in by the
// JobRegistry.
type JobRegistryListMsg struct {
	Jobs chan []string
}

// JobRegistryRemoveMsg is the struct used to remove JobSyncers from the registry.
type JobRegistryRemoveMsg struct {
	Syncer *JobSyncer
	Latch  chan int
}

// JobRegistry encapsulates a map associating a string with a *JobSyncer. This
// will let additional goroutines communicate with the goroutine running the
// job associated with the key. The key will most likely be a UUID. There should
// only be one instance on JobRegistry per jobrunner instance.
type JobRegistry struct {
	Setter   chan *JobRegistrySetMsg
	Getter   chan *JobRegistryGetMsg
	Remove   chan *JobRegistryRemoveMsg
	Lister   chan *JobRegistryListMsg
	Registry map[string]*JobSyncer
}

// NewJobRegistry creates a new instance of JobRegistry and returns a pointer to
// it. Does not make sure that only one instance is created.
func NewJobRegistry() *JobRegistry {
	return &JobRegistry{
		Setter:   make(chan *JobRegistrySetMsg),
		Getter:   make(chan *JobRegistryGetMsg),
		Remove:   make(chan *JobRegistryRemoveMsg),
		Lister:   make(chan *JobRegistryListMsg),
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
				v, ok := j.Registry[g.Key]
				if !ok {
					g.Resp <- nil
				} else {
					g.Resp <- v
				}
			case rem := <-j.Remove:
				for k, v := range j.Registry {
					if v == rem.Syncer {
						delete(j.Registry, k)
					}
					rem.Latch <- 1
				}
			case lister := <-j.Lister:
				var keys []string
				for k := range j.Registry {
					keys = append(keys, k)
				}
				lister.Jobs <- keys
			}
		}
	}()
}

// Register adds the *JobSyncer to the registry with the key set to the
// value of uuid.
func (j *JobRegistry) Register(uuid string, s *JobSyncer) {
	m := &JobRegistrySetMsg{
		Key:   uuid,
		Value: s,
		Latch: make(chan int),
	}
	j.Setter <- m
	<-m.Latch
}

// Get looks up the job for the given uuid in the registry.
func (j *JobRegistry) Get(uuid string) *JobSyncer {
	getMsg := &JobRegistryGetMsg{
		Key:  uuid,
		Resp: make(chan *JobSyncer),
	}
	j.Getter <- getMsg
	return <-getMsg.Resp
}

// HasKey returns true if a job associated with uuid is running, false otherwise.
func (j *JobRegistry) HasKey(uuid string) bool {
	getMsg := &JobRegistryGetMsg{
		Key:  uuid,
		Resp: make(chan *JobSyncer),
	}
	j.Getter <- getMsg
	r := <-getMsg.Resp
	if r == nil {
		return false
	}
	return true
}

// ListJobs returns the list of jobs from the registry.
func (j *JobRegistry) ListJobs() []string {
	lister := &JobRegistryListMsg{
		Jobs: make(chan []string),
	}
	j.Lister <- lister
	retval := <-lister.Jobs
	if retval == nil {
		return make([]string, 0)
	}
	return retval
}

// Delete deletes a *JobSyncer from the registry.
func (j *JobRegistry) Delete(s *JobSyncer) {
	msg := &JobRegistryRemoveMsg{
		Syncer: s,
		Latch:  make(chan int),
	}
	j.Remove <- msg
	<-msg.Latch
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
	jobID := uuid.New()
	j.Registry.Register(jobID, syncer)
	j.Execute(syncer)
	syncer.Command <- command
	syncer.Environment <- environment
	syncer.Start <- 1
	<-syncer.Started
	return jobID
}

func monitorJobState(s *JobSyncer, done chan<- error, abort <-chan int) {
	go func() {
		select {
		case <-s.Kill:
			s.Killed = true
			if s.CmdPtr != nil {
				s.CmdPtr.Process.Kill()
			}
			return
		case <-s.Completed:
			return
		case <-abort:
			return
		}
	}()
	go func() {
		select {
		case done <- s.CmdPtr.Wait():
		}
	}()
}

// Execute starts up a goroutine that communicates via a JobSyncer and will
// eventually execute a job.
func (j *JobExecutor) Execute(s *JobSyncer) {
	go func() {
		shouldStart := false
		running := false
		var cmdString string
		var environment map[string]string
		var cmd *exec.Cmd

		for {
			select {
			case cmdString = <-s.Command:
			case environment = <-s.Environment:
			case <-s.Start:
				shouldStart = true
			}

			if cmdString != "" && environment != nil && shouldStart && !running {
				break
			}
		}

		cmd = exec.Command("bash", "-c", cmdString)
		cmd.Env = formatEnv(environment)
		cmd.Stdout = s
		cmd.Stderr = s
		s.CmdPtr = cmd

		done := make(chan error)
		abort := make(chan int)

		err := cmd.Start()
		s.Started <- 1
		if err != nil {
			s.ExitCode = -1000
			abort <- 1
			return
		}

		monitorJobState(s, done, abort)

		go func() {
			defer s.Quit()
			defer j.Registry.Delete(s)
			select {
			case err := <-done:
				if s.Killed { //Job killed
					s.ExitCode = -100
					s.Completed <- 1
					return
				}
				if err == nil && s.ExitCode == -9000 { //Job exited normally
					if cmd.ProcessState != nil {
						s.ExitCode = exitCode(cmd)
					} else {
						s.ExitCode = 0
					}
				}
				if err != nil { //job exited badly, but wasn't killed
					if cmd.ProcessState != nil {
						s.ExitCode = exitCode(cmd)
					} else {
						s.ExitCode = 1
					}
				}
				s.Completed <- 1
				return
			}
		}()
	}()
}

// Kill terminates the specified job with extreme prejudice.
func (j *JobExecutor) Kill(uuid string) {
	if j.Registry.HasKey(uuid) {
		syncer := j.Registry.Get(uuid)
		syncer.Kill <- 1
	}
}

func formatEnv(env map[string]string) []string {
	output := make([]string, 1)
	for key, val := range env {
		output = append(output, fmt.Sprintf("%s=%s", key, val))
	}
	return output
}
