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

// OutputListener is the message struct for the Setter and Remove channels
// in a OutputRegistry instance. The Listener is the channel that job outputs
// are sent out on. The Latch channel is used for synchronizing the
// AddListener() and RemoveListener() calls.
type OutputListener struct {
	Listener   chan []byte
	Quit       chan int
	readBuffer []byte
}

// NewOutputListener returns a new instance of OutputListener.
func NewOutputListener() *OutputListener {
	return &OutputListener{
		Listener:   make(chan []byte),
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
	quitChannel chan int
	eof         bool
}

// NewOutputReader will create a new OutputReader with the given
// OutputListener.
func NewOutputReader(l *OutputListener) *OutputReader {
	r := &OutputReader{
		accum:       make([]byte, 0),
		listener:    l,
		m:           &sync.Mutex{},
		quitChannel: make(chan int),
		eof:         false,
	}
	go r.run()
	return r
}

func (r *OutputReader) run() {
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
			r.eof = true
			r.m.Unlock()
			break
		case <-r.quitChannel: //Quit if a caller tells us to.
			r.m.Lock()
			r.eof = true
			r.m.Unlock()
			break
		}
	}
}

// Reader will do a consuming read from the OutputReader's buffer. This makes
// it implement the Reader interface.
func (r *OutputReader) Read(p []byte) (n int, err error) {
	r.m.Lock()
	defer r.m.Unlock()
	if r.eof && len(r.accum) == 0 {
		return 0, io.EOF
	}
	if len(r.accum) == 0 {
		return 0, nil
	}
	var bytesRead int
	if len(r.accum) <= len(p) {
		bytesRead = copy(p, r.accum)
		r.accum = make([]byte, 0)
		if r.eof {
			return bytesRead, io.EOF
		}
		return bytesRead, nil
	}
	bytesRead = copy(p, r.accum)
	r.accum = r.accum[bytesRead:]
	if r.eof && (len(r.accum) <= 0) {
		return bytesRead, io.EOF
	}
	return bytesRead, nil

}

// Quit will tell the goroutine that pushes data into the buffer to quit.
func (r *OutputReader) Quit() {
	r.m.Lock()
	r.eof = true
	r.m.Unlock()
	r.quitChannel <- 1
}

// outputRegistry contains a list of channels that accept []byte's. Each job
// gets its own OutputRegistry. The OutputRegistry is referred to inside
// the job's Syncer instance, which in turn is referred to within the the
// Registry.
type outputRegistry struct {
	commands chan outputRegistryCmd
	Input    chan []byte
}

type outputRegistryAction int

const (
	registrySet outputRegistryAction = iota
	registryRemove
	registryFind
	registryQuit
)

type outputRegistryCmd struct {
	action outputRegistryAction
	key    *OutputListener
	value  chan []byte
	result chan interface{}
}

type outputRegistryFindResult struct {
	found bool
	value chan []byte
}

// NewOutputRegistry returns a pointer to a new instance of OutputRegistry.
func NewOutputRegistry() *outputRegistry {
	l := &outputRegistry{
		commands: make(chan outputRegistryCmd),
		Input:    make(chan []byte),
	}
	go l.run()
	return l
}

// Listen fires off a goroutine that can be communicated with through the Input,
// Setter, and Remove channels.
func (o *outputRegistry) run() {
	registry := make(map[*OutputListener]chan []byte)
	for {
		select {
		case command := <-o.commands:
			switch command.action {
			case registrySet:
				registry[command.key] = command.value
			case registryRemove:
				delete(registry, command.key)
				command.result <- 1
			case registryFind:
				val, ok := registry[command.key]
				command.result <- outputRegistryFindResult{found: ok, value: val}
			case registryQuit:
				for l := range registry {
					l.Quit <- 1
				}
				close(o.commands)
			}
		case buf := <-o.Input: //Demuxing the output to the listeners
			for _, ch := range registry {
				ch <- buf
			}
		}
	}
}

// AddListener creates a OutputListener, adds it to the OutputRegistry,
// and returns a pointer to the OutputListener. Synchronizes with the
// OutputRegistry goroutine through the OutputListener's Latch channel.
func (o *outputRegistry) AddListener() *OutputListener {
	adder := NewOutputListener()
	o.commands <- outputRegistryCmd{key: adder, value: adder.Listener, action: registrySet}
	return adder
}

// RemoveListener removes the passed in OutputListener from the
// OutputRegistry. Synchronizes with the JobOuputRegistry goroutine through
// the OutputListener's Latch channel. Does not close any channels.
func (o *outputRegistry) RemoveListener(l *OutputListener) {
	reply := make(chan interface{})
	cmd := outputRegistryCmd{key: l, action: registryRemove, result: reply}
	o.commands <- cmd
	<-reply
}

func (o *outputRegistry) HasKey(l *OutputListener) bool {
	reply := make(chan interface{})
	cmd := outputRegistryCmd{key: l, action: registryFind, result: reply}
	o.commands <- cmd
	result := (<-reply).(outputRegistryFindResult)
	return result.found
}

// Quit tells the OutputRegistry's goroutine to exit.
func (o *outputRegistry) Quit() {
	o.commands <- outputRegistryCmd{action: registryQuit}
}

// Syncer contains channels that can be used to communicate with a job
// goroutine. It also contains a pointer to a OutputRegistry.
type Syncer struct {
	cmds           chan syncerCmd
	Command        chan string
	Environment    chan map[string]string
	Start          chan int
	Started        chan int
	Kill           chan int
	killed         bool
	killedLock     *sync.Mutex
	Output         chan []byte
	exitCode       int
	exitCodeLock   *sync.Mutex
	Completed      chan int
	cmdPtr         *exec.Cmd
	cmdPtrLock     *sync.Mutex
	OutputRegistry *outputRegistry
	UUID           string
	uuidLock       *sync.Mutex
}

type syncerAction int

const (
	syncerQuit syncerAction = iota
)

type syncerCmd struct {
	action syncerAction
}

// NewSyncer creates a new instance of Syncer and returns a pointer to it.
func NewSyncer() *Syncer {
	s := &Syncer{
		cmds:           make(chan syncerCmd),
		Command:        make(chan string),
		Environment:    make(chan map[string]string),
		Start:          make(chan int),
		Started:        make(chan int),
		Kill:           make(chan int),
		killed:         false,
		killedLock:     &sync.Mutex{},
		Output:         make(chan []byte),
		exitCode:       -9000,
		exitCodeLock:   &sync.Mutex{},
		Completed:      make(chan int),
		cmdPtr:         nil,
		cmdPtrLock:     &sync.Mutex{},
		OutputRegistry: NewOutputRegistry(),
		UUID:           "",
		uuidLock:       &sync.Mutex{},
	}
	go s.run()
	return s
}

func (s *Syncer) run() {
	select {
	case <-s.cmds:
		s.OutputRegistry.Quit()
		close(s.cmds)

	}
}

// SetKilled sets the killed field for a Syncer. Should be threadsafe.
func (s *Syncer) SetKilled(k bool) {
	s.killedLock.Lock()
	s.killed = k
	s.killedLock.Unlock()
}

// GetKilled gets the killed field for a Syncer. Should be threadsafe.
func (s *Syncer) GetKilled() bool {
	var retval bool
	s.killedLock.Lock()
	retval = s.killed
	s.killedLock.Unlock()
	return retval
}

// SetExitCode sets the exitCode field for a Syncer. Should be threadsafe.
func (s *Syncer) SetExitCode(e int) {
	s.exitCodeLock.Lock()
	s.exitCode = e
	s.exitCodeLock.Unlock()
}

// GetExitCode gets the exitCode from a Syncer. Should be threadsafe.
func (s *Syncer) GetExitCode() int {
	var retval int
	s.exitCodeLock.Lock()
	retval = s.exitCode
	s.exitCodeLock.Unlock()
	return retval
}

// SetCmdPtr sets the pointer to an exec.Cmd instance for the Syncer. Should
// be threadsafe.
func (s *Syncer) SetCmdPtr(p *exec.Cmd) {
	s.cmdPtrLock.Lock()
	s.cmdPtr = p
	s.cmdPtrLock.Unlock()
}

// GetCmdPtr gets the pointer to an exec.Cmd instance that's associated with
// the Syncer. Should be threadsafe, but don't don't mutate any state on the
// pointer or bad things could happen.
func (s *Syncer) GetCmdPtr() *exec.Cmd {
	var retval *exec.Cmd
	s.cmdPtrLock.Lock()
	retval = s.cmdPtr
	s.cmdPtrLock.Unlock()
	return retval
}

// SetUUID sets the UUID for a Syncer.
func (s *Syncer) SetUUID(uuid string) {
	s.uuidLock.Lock()
	s.UUID = uuid
	s.uuidLock.Unlock()
}

// GetUUID gets the UUID for a Syncer.
func (s *Syncer) GetUUID() string {
	var retval string
	s.uuidLock.Lock()
	retval = s.UUID
	s.uuidLock.Unlock()
	return retval
}

// Write sends the []byte array passed out on RoutineWriter's OutChannel
func (s *Syncer) Write(p []byte) (n int, err error) {
	s.OutputRegistry.Input <- p
	return len(p), nil
}

// Quit tells the Syncer to clean up its OutputRegistry
func (s *Syncer) Quit() {
	s.cmds <- syncerCmd{action: syncerQuit}
}

// RegistryCmd represents a command sent to the registry
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

// Registry encapsulates a map associating a string with a *Syncer. This
// will let additional goroutines communicate with the goroutine running the
// job associated with the key. The key will most likely be a UUID. There should
// only be one instance on Registry per jobrunner instance.
type Registry chan registryCommand

// NewRegistry creates a new instance of Registry and returns a pointer to
// it. Does not make sure that only one instance is created.
func NewRegistry() Registry {
	r := make(Registry)
	go r.run()
	return r
}

type registryFindResult struct {
	found  bool
	result *Syncer
}

// Listen launches a goroutine that can be communicated with via the setter
// and getter channels passed in. Listen is non-blocking.
func (r Registry) run() {
	reg := make(map[string]*Syncer)
	for command := range r {
		switch command.action {
		case set:
			reg[command.key] = command.value.(*Syncer)
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

// Register adds the *Syncer to the registry with the key set to the
// value of uuid.
func (r Registry) Register(uuid string, s *Syncer) {
	r <- registryCommand{action: set, key: uuid, value: s}
}

// Get looks up the job for the given uuid in the registry.
func (r Registry) Get(uuid string) *Syncer {
	reply := make(chan interface{})
	regCmd := registryCommand{action: get, key: uuid, result: reply}
	r <- regCmd
	result := (<-reply).(registryFindResult)
	return result.result
}

// HasKey returns true if a job associated with uuid is running, false otherwise.
func (r Registry) HasKey(uuid string) bool {
	reply := make(chan interface{})
	regCmd := registryCommand{action: get, key: uuid, result: reply}
	r <- regCmd
	result := (<-reply).(registryFindResult)
	return result.found
}

// List returns the list of jobs from the registry.
func (r Registry) List() []string {
	reply := make(chan interface{})
	regCmd := registryCommand{action: listkeys, result: reply}
	r <- regCmd
	result := (<-reply).([]string)
	return result
}

// Delete deletes a *Syncer from the registry.
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
		uuid := s.GetUUID()
		for {
			select {
			case <-s.Kill:
				s.SetKilled(true)
				cmdptr := s.GetCmdPtr()
				if cmdptr != nil {
					cmdptr.Process.Kill()
				}
				log.Printf("Kill signal was sent to job %s.", uuid)
			case <-s.Completed:
				log.Printf("Job %s completed.", uuid)
				return
			case <-abort:
				log.Printf("Abort was sent for job %s.", uuid)
				return
			}
		}
	}()
	go func() {
		cmdptr := s.GetCmdPtr()
		uuid := s.GetUUID()
		select {
		case done <- cmdptr.Wait():
			log.Printf("Job %s is no longer in the Wait state.", uuid)
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
		uuid := s.GetUUID()
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
		s.SetCmdPtr(cmd)
		done := make(chan error)
		abort := make(chan int)
		err := cmd.Start()
		s.Started <- 1
		if err != nil {
			s.SetExitCode(-1000)
			abort <- 1
			return
		}
		log.Printf("Started job %s.", uuid)
		monitorJobState(s, done, abort)
		go func() {
			defer s.Quit()
			defer j.Registry.Delete(uuid)
			select {
			case err := <-done:
				if s.GetKilled() { //Job killed
					s.SetExitCode(-100)
					s.Completed <- 1
					return
				}
				if err == nil && s.GetExitCode() == -9000 { //Job exited normally
					if cmd.ProcessState != nil {
						s.SetExitCode(exitCode(cmd))
					} else {
						s.SetExitCode(0)
					}
				}
				if err != nil { //job exited badly, but wasn't killed
					if cmd.ProcessState != nil {
						s.SetExitCode(exitCode(cmd))
					} else {
						s.SetExitCode(1)
					}
				}
				log.Printf("Job %s exited with a status of %d.", uuid, s.GetExitCode())
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
