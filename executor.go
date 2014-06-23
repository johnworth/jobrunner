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

// Job contains channels that can be used to communicate with a job
// goroutine. It also contains a pointer to a OutputRegistry.
type Job struct {
	cmds           chan jobCmd
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

type jobAction int

const (
	jobQuit jobAction = iota
)

type jobCmd struct {
	action jobAction
}

// NewJob creates a new instance of Job and returns a pointer to it.
func NewJob() *Job {
	j := &Job{
		cmds:           make(chan jobCmd),
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
	go j.run()
	return j
}

func (j *Job) run() {
	select {
	case <-j.cmds:
		j.OutputRegistry.Quit()
		close(j.cmds)

	}
}

// SetKilled sets the killed field for a Job. Should be threadsafe.
func (j *Job) SetKilled(k bool) {
	j.killedLock.Lock()
	j.killed = k
	j.killedLock.Unlock()
}

// GetKilled gets the killed field for a Job. Should be threadsafe.
func (j *Job) GetKilled() bool {
	var retval bool
	j.killedLock.Lock()
	retval = j.killed
	j.killedLock.Unlock()
	return retval
}

// SetExitCode sets the exitCode field for a Job. Should be threadsafe.
func (j *Job) SetExitCode(e int) {
	j.exitCodeLock.Lock()
	j.exitCode = e
	j.exitCodeLock.Unlock()
}

// GetExitCode gets the exitCode from a Job. Should be threadsafe.
func (j *Job) GetExitCode() int {
	var retval int
	j.exitCodeLock.Lock()
	retval = j.exitCode
	j.exitCodeLock.Unlock()
	return retval
}

// SetCmdPtr sets the pointer to an exec.Cmd instance for the Job. Should
// be threadsafe.
func (j *Job) SetCmdPtr(p *exec.Cmd) {
	j.cmdPtrLock.Lock()
	j.cmdPtr = p
	j.cmdPtrLock.Unlock()
}

// GetCmdPtr gets the pointer to an exec.Cmd instance that's associated with
// the Job. Should be threadsafe, but don't don't mutate any state on the
// pointer or bad things could happen.
func (j *Job) GetCmdPtr() *exec.Cmd {
	var retval *exec.Cmd
	j.cmdPtrLock.Lock()
	retval = j.cmdPtr
	j.cmdPtrLock.Unlock()
	return retval
}

// SetUUID sets the UUID for a Job.
func (j *Job) SetUUID(uuid string) {
	j.uuidLock.Lock()
	j.UUID = uuid
	j.uuidLock.Unlock()
}

// GetUUID gets the UUID for a Job.
func (j *Job) GetUUID() string {
	var retval string
	j.uuidLock.Lock()
	retval = j.UUID
	j.uuidLock.Unlock()
	return retval
}

// Write sends the []byte array passed out on RoutineWriter's OutChannel
func (j *Job) Write(p []byte) (n int, err error) {
	j.OutputRegistry.Input <- p
	return len(p), nil
}

// Quit tells the Job to clean up its OutputRegistry
func (j *Job) Quit() {
	j.cmds <- jobCmd{action: jobQuit}
}

func (j *Job) monitorJobState(done chan<- error, abort <-chan int) {
	go func() {
		uuid := j.GetUUID()
		for {
			select {
			case <-j.Kill:
				j.SetKilled(true)
				cmdptr := j.GetCmdPtr()
				if cmdptr != nil {
					cmdptr.Process.Kill()
				}
				log.Printf("Kill signal was sent to job %s.", uuid)
			case <-j.Completed:
				log.Printf("Job %s completed.", uuid)
				return
			case <-abort:
				log.Printf("Abort was sent for job %s.", uuid)
				return
			}
		}
	}()
	go func() {
		cmdptr := j.GetCmdPtr()
		uuid := j.GetUUID()
		select {
		case done <- cmdptr.Wait():
			log.Printf("Job %s is no longer in the Wait state.", uuid)
		}
	}()
}

func (j *Job) waitForJob(e *Executor, done <-chan error) {
	uuid := j.GetUUID()
	defer j.Quit()
	defer e.Registry.Delete(uuid)
	cmd := j.GetCmdPtr()
	select {
	case err := <-done:
		if j.GetKilled() { //Job killed
			j.SetExitCode(-100)
			j.Completed <- 1
			return
		}
		if err == nil && j.GetExitCode() == -9000 { //Job exited normally
			if cmd.ProcessState != nil {
				j.SetExitCode(exitCode(cmd))
			} else {
				j.SetExitCode(0)
			}
		}
		if err != nil { //job exited badly, but wasn't killed
			if cmd.ProcessState != nil {
				j.SetExitCode(exitCode(cmd))
			} else {
				j.SetExitCode(1)
			}
		}
		log.Printf("Job %s exited with a status of %d.", uuid, j.GetExitCode())
		j.Completed <- 1
		return
	}
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

// Launch fires off a new job, adding its Job instance to the job registry.
func (e *Executor) Launch(command string, environment map[string]string) string {
	job := NewJob()
	jobID := uuid.New()
	job.UUID = jobID
	e.Registry.Register(jobID, job)
	log.Printf("Registering job %s.", jobID)
	e.Execute(job)
	job.Command <- command
	job.Environment <- environment
	job.Start <- 1
	<-job.Started
	return jobID
}

// Execute starts up a goroutine that communicates via a Job and will
// eventually execute a job.
func (e *Executor) Execute(j *Job) {
	log.Printf("Executing job %s.", j.UUID)
	go func() {
		shouldStart := false
		running := false
		uuid := j.GetUUID()
		var cmdString string
		var environment map[string]string
		var cmd *exec.Cmd
		for {
			select {
			case cmdString = <-j.Command:
			case environment = <-j.Environment:
			case <-j.Start:
				shouldStart = true
			}

			if cmdString != "" && environment != nil && shouldStart && !running {
				break
			}
		}
		cmd = exec.Command("bash", "-c", cmdString)
		cmd.Env = formatEnv(environment)
		cmd.Stdout = j
		cmd.Stderr = j
		j.SetCmdPtr(cmd)
		done := make(chan error)
		abort := make(chan int)
		err := cmd.Start()
		j.Started <- 1
		if err != nil {
			j.SetExitCode(-1000)
			abort <- 1
			return
		}
		log.Printf("Started job %s.", uuid)
		j.monitorJobState(done, abort)
		go j.waitForJob(e, done) //Execute needs to be non-blocking.
	}()
}

// Kill terminates the specified job with extreme prejudice.
func (e *Executor) Kill(uuid string) {
	if e.Registry.HasKey(uuid) {
		job := e.Registry.Get(uuid)
		job.Kill <- 1
	}
}

func formatEnv(env map[string]string) []string {
	output := make([]string, 1)
	for key, val := range env {
		output = append(output, fmt.Sprintf("%s=%s", key, val))
	}
	return output
}
