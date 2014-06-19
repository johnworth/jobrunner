package main

import (
	"fmt"
	"io"
	"log"
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

// OutputQuitMsg is sent to the OutputRegistry on the Quit channel when
// a job completes. The OutputRegistry then tells all of the registered
// OutputListeners to quit.
type OutputQuitMsg struct {
	Latch chan int
}

// OutputListener is the message struct for the Setter and Remove channels
// in a OutputRegistry instance. The Listener is the channel that job outputs
// are sent out on. The Latch channel is used for synchronizing the
// AddListener() and RemoveListener() calls.
type OutputListener struct {
	Listener   chan []byte
	Latch      chan int
	Quit       chan int
	readBuffer []byte
}

// NewOutputListener returns a new instance of OutputListener.
func NewOutputListener() *OutputListener {
	return &OutputListener{
		Listener:   make(chan []byte),
		Latch:      make(chan int),
		Quit:       make(chan int),
		readBuffer: make([]byte, 0),
	}
}

// OutputReader will buffer and allow Read()s from data sent via a
// OutputListener
type OutputReader struct {
	accum       []byte
	listener    *OutputListener
	m           *sync.Mutex
	QuitChannel chan int
	EOF         bool
}

// NewOutputReader will create a new OutputReader with the given
// OutputListener.
func NewOutputReader(l *OutputListener) *OutputReader {
	r := &OutputReader{
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

// Reader will do a consuming read from the OutputReader's buffer. This makes
// it implement the Reader interface.
func (r *OutputReader) Read(p []byte) (n int, err error) {
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
func (r *OutputReader) Quit() {
	r.m.Lock()
	r.EOF = true
	r.m.Unlock()
	r.QuitChannel <- 1
}

// OutputRegistry contains a list of channels that accept []byte's. Each job
// gets its own OutputRegistry. The OutputRegistry is referred to inside
// the job's Syncer instance, which in turn is referred to within the the
// Registry.
type OutputRegistry struct {
	Input       chan []byte
	Setter      chan *OutputListener
	Remove      chan *OutputListener
	Registry    map[*OutputListener]chan []byte
	QuitChannel chan *OutputQuitMsg
}

// NewOutputRegistry returns a pointer to a new instance of OutputRegistry.
func NewOutputRegistry() *OutputRegistry {
	l := &OutputRegistry{
		Input:       make(chan []byte),
		Setter:      make(chan *OutputListener),
		Remove:      make(chan *OutputListener),
		Registry:    make(map[*OutputListener]chan []byte),
		QuitChannel: make(chan *OutputQuitMsg),
	}
	l.Listen()
	return l
}

// Listen fires off a goroutine that can be communicated with through the Input,
// Setter, and Remove channels.
func (j *OutputRegistry) Listen() {
	go func() {
		for {
			select {
			case a := <-j.Setter: //Adding a Listener
				j.Registry[a] = a.Listener
				a.Latch <- 1
			case buf := <-j.Input: //Demuxing the output to the listeners
				for _, ch := range j.Registry {
					ch <- buf
				}
			case rem := <-j.Remove: //Removing a listener
				delete(j.Registry, rem)
				rem.Latch <- 1
			case q := <-j.QuitChannel: //Demuxing a Quit command to the listeners.
				for l := range j.Registry {
					l.Quit <- 1
				}
				q.Latch <- 1
				break
			}
		}
	}()
}

// AddListener creates a OutputListener, adds it to the OutputRegistry,
// and returns a pointer to the OutputListener. Synchronizes with the
// OutputRegistry goroutine through the OutputListener's Latch channel.
func (j *OutputRegistry) AddListener() *OutputListener {
	listener := make(chan []byte)
	latch := make(chan int)
	adder := &OutputListener{
		Listener:   listener,
		Latch:      latch,
		Quit:       make(chan int),
		readBuffer: make([]byte, 0),
	}
	j.Setter <- adder
	<-latch
	return adder
}

// RemoveListener removes the passed in OutputListener from the
// OutputRegistry. Synchronizes with the JobOuputRegistry goroutine through
// the OutputListener's Latch channel. Does not close any channels.
func (j *OutputRegistry) RemoveListener(l *OutputListener) {
	j.Remove <- l
	<-l.Latch
}

// Quit tells the OutputRegistry's goroutine to exit.
func (j *OutputRegistry) Quit() {
	msg := &OutputQuitMsg{
		Latch: make(chan int),
	}
	j.QuitChannel <- msg
	<-msg.Latch
}

// Syncer contains channels that can be used to communicate with a job
// goroutine. It also contains a pointer to a OutputRegistry.
type Syncer struct {
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
	OutputRegistry *OutputRegistry
	UUID           string
}

// NewSyncer creates a new instance of Syncer and returns a pointer to it.
func NewSyncer() *Syncer {
	s := &Syncer{
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
		OutputRegistry: NewOutputRegistry(),
		UUID:           "",
	}
	s.OutputRegistry.Listen()
	return s
}

// Write sends the []byte array passed out on RoutineWriter's OutChannel
func (j *Syncer) Write(p []byte) (n int, err error) {
	j.OutputRegistry.Input <- p
	return len(p), nil
}

// Quit tells the Syncer to clean up its OutputRegistry
func (j *Syncer) Quit() {
	j.OutputRegistry.Quit()
}

// RegistryGetMsg wraps a key that you want to retrieve from a Registry
// and a Resp channel that the retrieved value should be sent back on.
type RegistryGetMsg struct {
	Key  string
	Resp chan *Syncer
}

// RegistrySetMsg wraps a key that you want to set, the Value (a *Syncer)
// that should be associated with the key, and a Latch channel that will be
// sent an integer when the Value has been set.
type RegistrySetMsg struct {
	Key   string
	Value *Syncer
	Latch chan int
}

// RegistryListMsg is the struct that job UUIDs are returned in by the
// Registry.
type RegistryListMsg struct {
	Jobs chan []string
}

// RegistryRemoveMsg is the struct used to remove Syncers from the registry.
type RegistryRemoveMsg struct {
	Syncer *Syncer
	Latch  chan int
}

// Registry encapsulates a map associating a string with a *Syncer. This
// will let additional goroutines communicate with the goroutine running the
// job associated with the key. The key will most likely be a UUID. There should
// only be one instance on Registry per jobrunner instance.
type Registry struct {
	Setter   chan *RegistrySetMsg
	Getter   chan *RegistryGetMsg
	Remove   chan *RegistryRemoveMsg
	Lister   chan *RegistryListMsg
	Registry map[string]*Syncer
}

// NewRegistry creates a new instance of Registry and returns a pointer to
// it. Does not make sure that only one instance is created.
func NewRegistry() *Registry {
	r := &Registry{
		Setter:   make(chan *RegistrySetMsg),
		Getter:   make(chan *RegistryGetMsg),
		Remove:   make(chan *RegistryRemoveMsg),
		Lister:   make(chan *RegistryListMsg),
		Registry: make(map[string]*Syncer),
	}
	r.Listen()
	return r
}

// Listen launches a goroutine that can be communicated with via the setter
// and getter channels passed in. Listen is non-blocking.
func (j *Registry) Listen() {
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

// Register adds the *Syncer to the registry with the key set to the
// value of uuid.
func (j *Registry) Register(uuid string, s *Syncer) {
	m := &RegistrySetMsg{
		Key:   uuid,
		Value: s,
		Latch: make(chan int),
	}
	j.Setter <- m
	<-m.Latch
}

// Get looks up the job for the given uuid in the registry.
func (j *Registry) Get(uuid string) *Syncer {
	getMsg := &RegistryGetMsg{
		Key:  uuid,
		Resp: make(chan *Syncer),
	}
	j.Getter <- getMsg
	return <-getMsg.Resp
}

// HasKey returns true if a job associated with uuid is running, false otherwise.
func (j *Registry) HasKey(uuid string) bool {
	getMsg := &RegistryGetMsg{
		Key:  uuid,
		Resp: make(chan *Syncer),
	}
	j.Getter <- getMsg
	r := <-getMsg.Resp
	if r == nil {
		return false
	}
	return true
}

// List returns the list of jobs from the registry.
func (j *Registry) List() []string {
	lister := &RegistryListMsg{
		Jobs: make(chan []string),
	}
	j.Lister <- lister
	retval := <-lister.Jobs
	if retval == nil {
		return make([]string, 0)
	}
	return retval
}

// Delete deletes a *Syncer from the registry.
func (j *Registry) Delete(s *Syncer) {
	msg := &RegistryRemoveMsg{
		Syncer: s,
		Latch:  make(chan int),
	}
	j.Remove <- msg
	<-msg.Latch
}

// Executor maintains a reference to a Registry and is able to launch
// jobs. There should only be one instance of Executor per instance of
// jobrunner, but there isn't anything to prevent you from creating more than
// one.
type Executor struct {
	Registry *Registry
}

// NewExecutor creates a new instance of Executor and returns a pointer to
// it.
func NewExecutor() *Executor {
	e := &Executor{
		Registry: NewRegistry(),
	}
	return e
}

// Launch fires off a new job, adding its Syncer instance to the job registry.
func (j *Executor) Launch(command string, environment map[string]string) string {
	syncer := NewSyncer()
	jobID := uuid.New()
	syncer.UUID = jobID
	j.Registry.Register(jobID, syncer)
	log.Printf("Registering job %s.", jobID)
	j.Execute(syncer)
	syncer.Command <- command
	syncer.Environment <- environment
	syncer.Start <- 1
	<-syncer.Started
	return jobID
}

func monitorJobState(s *Syncer, done chan<- error, abort <-chan int) {
	go func() {
		for {
			select {
			case <-s.Kill:
				s.Killed = true
				if s.CmdPtr != nil {
					s.CmdPtr.Process.Kill()
				}
				log.Printf("Kill signal was sent to job %s.", s.UUID)
			case <-s.Completed:
				log.Printf("Job %s completed.", s.UUID)
				return
			case <-abort:
				log.Printf("Abort was sent for job %s.", s.UUID)
				return
			}
		}
	}()
	go func() {
		select {
		case done <- s.CmdPtr.Wait():
			log.Printf("Job %s is no longer in the Wait state.", s.UUID)
		}
	}()
}

// Execute starts up a goroutine that communicates via a Syncer and will
// eventually execute a job.
func (j *Executor) Execute(s *Syncer) {
	log.Printf("Executing job %s.", s.UUID)
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
		log.Printf("Started job %s.", s.UUID)
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
				log.Printf("Job %s exited with a status of %d.", s.UUID, s.ExitCode)
				s.Completed <- 1
				return
			}
		}()
	}()
}

// Kill terminates the specified job with extreme prejudice.
func (j *Executor) Kill(uuid string) {
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
