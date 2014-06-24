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

// OutputListener maintains a Listener and Quit channel for data read from
// a job's stdout/stderr stream.
type OutputListener struct {
	Listener chan []byte
	Quit     chan int
}

// NewOutputListener returns a new instance of OutputListener.
func NewOutputListener() *OutputListener {
	return &OutputListener{
		Listener: make(chan []byte),
		Quit:     make(chan int),
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

// Reader will do a consuming read from the OutputReader's buffer. This method
// allows an OutputReader to be substituted for an io.Reader.
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

// Job contains all of the state associated with a job. You'll primarily
// interact with a job through methods associated with Job.
type Job struct {
	cmds           chan jobCmd
	command        chan string
	done           chan error
	abort          chan int
	environment    chan map[string]string
	begin          chan int
	began          chan int
	kill           chan int
	killed         bool
	killedLock     *sync.Mutex
	Output         chan []byte
	exitCode       int
	exitCodeLock   *sync.Mutex
	completed      chan int
	cmdPtr         *exec.Cmd
	cmdPtrLock     *sync.Mutex
	OutputRegistry *outputRegistry
	uuid           string
	uuidLock       *sync.Mutex
}

type jobAction int

const (
	jobQuit jobAction = iota
)

type jobCmd struct {
	action jobAction
}

// NewJob returns a pointer to a new instance of Job.
func NewJob() *Job {
	j := &Job{
		cmds:           make(chan jobCmd),
		command:        make(chan string),
		done:           make(chan error),
		abort:          make(chan int),
		environment:    make(chan map[string]string),
		begin:          make(chan int),
		began:          make(chan int),
		kill:           make(chan int),
		killed:         false,
		killedLock:     &sync.Mutex{},
		Output:         make(chan []byte),
		exitCode:       -9000,
		exitCodeLock:   &sync.Mutex{},
		completed:      make(chan int),
		cmdPtr:         nil,
		cmdPtrLock:     &sync.Mutex{},
		OutputRegistry: NewOutputRegistry(),
		uuid:           "",
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

// Killed gets the killed field for a Job. Should be threadsafe.
func (j *Job) Killed() bool {
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

// ExitCode gets the exitCode from a Job. Should be threadsafe.
func (j *Job) ExitCode() int {
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

// CmdPtr gets the pointer to an exec.Cmd instance that's associated with
// the Job. Should be threadsafe, but don't don't mutate any state on the
// returned pointer or bad things could happen.
func (j *Job) CmdPtr() *exec.Cmd {
	var retval *exec.Cmd
	j.cmdPtrLock.Lock()
	retval = j.cmdPtr
	j.cmdPtrLock.Unlock()
	return retval
}

// SetUUID sets the UUID for a Job.
func (j *Job) SetUUID(uuid string) {
	j.uuidLock.Lock()
	j.uuid = uuid
	j.uuidLock.Unlock()
}

// UUID gets the UUID for a Job.
func (j *Job) UUID() string {
	var retval string
	j.uuidLock.Lock()
	retval = j.uuid
	j.uuidLock.Unlock()
	return retval
}

// Write sends the []byte array passed out on RoutineWriter's OutChannel. This
// should allow a Job instance to replace an io.Writer.
func (j *Job) Write(p []byte) (n int, err error) {
	j.OutputRegistry.Input <- p
	return len(p), nil
}

// Quit tells the Job to clean up after itself.
func (j *Job) Quit() {
	j.cmds <- jobCmd{action: jobQuit}
}

// MonitorState fires off two goroutines: one that waits for a message on the
// Kill, Completed, or abort channels, and another one that calls Wait() on
// the command that's running. Do not call this method before Start().
func (j *Job) MonitorState() {
	go func() {
		uuid := j.UUID()
		for {
			select {
			case <-j.kill:
				j.SetKilled(true)
				cmdptr := j.CmdPtr()
				if cmdptr != nil {
					cmdptr.Process.Kill()
				}
				log.Printf("Kill signal was sent to job %s.", uuid)
			case <-j.completed:
				log.Printf("Job %s completed.", uuid)
				return
			case <-j.abort:
				log.Printf("Abort was sent for job %s.", uuid)
				return
			}
		}
	}()
	go func() {
		cmdptr := j.CmdPtr()
		uuid := j.UUID()
		select {
		case j.done <- cmdptr.Wait():
			log.Printf("Job %s is no longer in the Wait state.", uuid)
		}
	}()
}

// Wait blocks until the running job is completed.
func (j *Job) Wait() {
	defer j.Quit()
	uuid := j.UUID()
	cmd := j.CmdPtr()
	select {
	case err := <-j.done:
		if j.Killed() { //Job killed
			j.SetExitCode(-100)
			j.completed <- 1
			return
		}
		if err == nil && j.ExitCode() == -9000 { //Job exited normally
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
		log.Printf("Job %s exited with a status of %d.", uuid, j.ExitCode())
		j.completed <- 1
		return
	}
}

// Start gets the job running. The job will not begin until a command is set,
// the environment is set, and the begin channel receives a message. The best
// way to do all that is with the Prepare() method.
func (j *Job) Start() {
	shouldStart := false
	var cmdString string
	var environment map[string]string
	var cmd *exec.Cmd
	for {
		select {
		case cmdString = <-j.command:
		case environment = <-j.environment:
		case <-j.begin:
			shouldStart = true
		}

		if cmdString != "" && environment != nil && shouldStart {
			break
		}
	}
	cmd = exec.Command("bash", "-c", cmdString)
	cmd.Env = formatEnv(environment)
	cmd.Stdout = j
	cmd.Stderr = j
	j.SetCmdPtr(cmd)
	err := cmd.Start()
	j.began <- 1
	if err != nil {
		j.SetExitCode(-1000)
		j.abort <- 1
		return
	}
	log.Printf("Started job %s.", j.UUID())
}

// Prepare allows the caller to set the command and environment for the job.
// Call this before or after Start(). It doesn't matter when, but it must be
// called.
func (j *Job) Prepare(command string, environment map[string]string) {
	go func() {
		j.command <- command
		j.environment <- environment
		j.begin <- 1
		<-j.began
	}()
}

// Kill kills a job.
func (j *Job) Kill() {
	j.kill <- 1
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

// Launch fires off a new job, adding a Job instance to the job registry.
func (e *Executor) Launch(command string, environment map[string]string) string {
	job := NewJob()
	jobID := uuid.New()
	job.SetUUID(jobID)
	e.Registry.Register(jobID, job)
	log.Printf("Registering job %s.", jobID)
	job.Prepare(command, environment)
	e.Execute(job)
	return jobID
}

// Execute fires off a goroutine that calls a jobs Start(), MonitorState(), and
// Wait() methods. Execute itself does not block.
func (e *Executor) Execute(j *Job) {
	log.Printf("Executing job %s.", j.UUID())
	go func() {
		uuid := j.UUID()
		defer e.Registry.Delete(uuid)
		j.Start()
		j.MonitorState()
		j.Wait()
	}()
}

// Kill terminates the specified job with extreme prejudice.
func (e *Executor) Kill(uuid string) {
	if e.Registry.HasKey(uuid) {
		job := e.Registry.Get(uuid)
		job.Kill()
	}
}

func formatEnv(env map[string]string) []string {
	output := make([]string, 1)
	for key, val := range env {
		output = append(output, fmt.Sprintf("%s=%s", key, val))
	}
	return output
}
