package jobs

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"sync"
	"syscall"

	"github.com/johnworth/jobrunner/config"

	"code.google.com/p/go-uuid/uuid"
)

// exitCode returns the integer exit code of a command. Start() and Wait()
// should have already been called on the Command.
func exitCode(cmd *exec.Cmd) int {
	return cmd.ProcessState.Sys().(syscall.WaitStatus).ExitStatus()
}

func formatEnv(env map[string]string) []string {
	output := make([]string, 1)
	for key, val := range env {
		output = append(output, fmt.Sprintf("%s=%s", key, val))
	}
	return output
}

// JobCommand is an interface for any commands that can be executed as part of
// a job.
type JobCommand interface {
	SetUUID(uuid string)
	UUID() string
	MonitorState()
	Wait()
	Start()
	Prepare(c string, e map[string]string) error
	Kill()
}

// Job is a type that contains one or more JobCommands to be run.
type Job struct {
	commands       []JobCommand //List of job commands to be run.
	commandsLock   *sync.RWMutex
	current        string //UUID of the currently running command.
	currentCommand *JobCommand
	currentLock    *sync.RWMutex
	uuid           string //UUID of the Job
	uuidLock       *sync.RWMutex
	completed      bool
	completedLock  *sync.RWMutex
	workingDir     string
	workingDirLock *sync.RWMutex
}

// NewJob returns a pointer to a new instance of Job.
func NewJob() *Job {
	newUUID := uuid.New()
	return &Job{
		commands:       make([]JobCommand, 0),
		commandsLock:   &sync.RWMutex{},
		currentCommand: nil,
		currentLock:    &sync.RWMutex{},
		uuid:           newUUID,
		uuidLock:       &sync.RWMutex{},
		completed:      false,
		completedLock:  &sync.RWMutex{},
		workingDir:     "",
		workingDirLock: &sync.RWMutex{},
	}
}

//Prepare performs operations prior to a Job's execution that it needs to succeed.
//Right now that consists of creating the working directory.
func (j *Job) Prepare() error {
	cfg := config.Get()
	err := os.Mkdir(path.Join(cfg.BaseDir, j.UUID()), 0755)
	return err
}

//WorkingDir returns the working directory set for a Job.
func (j *Job) WorkingDir() string {
	j.workingDirLock.RLock()
	defer j.workingDirLock.RUnlock()
	return j.workingDir
}

//SetWorkingDir sets the working directory for a Job.
func (j *Job) SetWorkingDir(w string) {
	j.workingDirLock.Lock()
	defer j.workingDirLock.Unlock()
	j.workingDir = w
}

// Commands returns a copy of the JobCommand list.
func (j *Job) Commands() []JobCommand {
	j.commandsLock.RLock()
	defer j.commandsLock.RUnlock()
	return j.commands
}

// AddCommand appends a new JobCommand to the command list.
func (j *Job) AddCommand(c JobCommand) {
	j.commandsLock.Lock()
	defer j.commandsLock.Unlock()
	j.commands = append(j.commands, c)
}

// Current returns the UUID of the current running command.
func (j *Job) Current() string {
	j.currentLock.RLock()
	defer j.currentLock.RUnlock()
	return j.current
}

// SetCurrent sets the UUID of the current running command.
func (j *Job) SetCurrent(c JobCommand) {
	j.currentLock.Lock()
	defer j.currentLock.Unlock()
	j.currentCommand = &c
}

// UUID returns the UUID of the Job.
func (j *Job) UUID() string {
	j.uuidLock.RLock()
	defer j.uuidLock.RUnlock()
	return j.uuid
}

// SetUUID sets the UUID of the Job.
func (j *Job) SetUUID(u string) {
	j.uuidLock.Lock()
	defer j.uuidLock.Unlock()
	j.uuid = u
}

// Completed returns true if the Job is done.
func (j *Job) Completed() bool {
	j.completedLock.RLock()
	defer j.completedLock.RUnlock()
	return j.completed
}

// SetCompleted sets the completion value.
func (j *Job) SetCompleted(c bool) {
	j.completedLock.Lock()
	defer j.completedLock.Unlock()
	j.completed = c
}

// Kill sends the currently running job a SIGKILL and shuts down the output
// registry.
func (j *Job) Kill() {
	cc := *j.currentCommand
	cc.Kill()
}

// Clean tells the job to delete its working directory. You should probably call
// Kill() first, otherwise bad things may happen.
func (j *Job) Clean() error {
	err := os.RemoveAll(j.WorkingDir())
	return err
}

// Run fires off a goroutine that iterates through the commands list and runs
// them one after the other. Prepare() should have been called on each command
// before Run() is called.
func (j *Job) Run() {
	for _, c := range j.Commands() {
		j.SetCurrent(c)
		c.Start()
		c.MonitorState()
		c.Wait()
	}
	j.SetCompleted(true)
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// PathResolve returns a []byte representing either a directory listing or a
// file download.
func (j *Job) PathResolve(fpath string) (*os.File, error) {
	wdir := j.WorkingDir()
	resolvedPath := path.Join(wdir, fpath)
	exists, err := pathExists(resolvedPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("%s does not exist", fpath)
	}
	opened, err := os.Open(resolvedPath)
	if err != nil {
		return nil, err
	}
	return opened, err
}

// BashCommand contains all of the state associated with a command run through
// bash.
type BashCommand struct {
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
	uuid           string
	uuidLock       *sync.Mutex
	workingDir     string
	workingDirLock *sync.Mutex
}

// NewBashCommand returns a pointer to a new instance of BashCommand.
func NewBashCommand() *BashCommand {
	newUUID := uuid.New()
	j := &BashCommand{
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
		uuid:           newUUID,
		uuidLock:       &sync.Mutex{},
		workingDir:     "",
		workingDirLock: &sync.Mutex{},
	}
	return j
}

// SetWorkingDir sets the working directory for a BashCommand. Should be threadsafe.
func (j *BashCommand) SetWorkingDir(w string) {
	j.workingDirLock.Lock()
	defer j.workingDirLock.Unlock()
	j.workingDir = w
}

// WorkingDir returns the working directory for a BashCommand. Should be threadsafe.
func (j *BashCommand) WorkingDir() string {
	j.workingDirLock.Lock()
	defer j.workingDirLock.Unlock()
	return j.workingDir
}

// SetKilled sets the killed field for a BashCommand. Should be threadsafe.
func (j *BashCommand) SetKilled(k bool) {
	j.killedLock.Lock()
	j.killed = k
	j.killedLock.Unlock()
}

// Killed gets the killed field for a BashCommand. Should be threadsafe.
func (j *BashCommand) Killed() bool {
	var retval bool
	j.killedLock.Lock()
	retval = j.killed
	j.killedLock.Unlock()
	return retval
}

// SetExitCode sets the exitCode field for a BashCommand. Should be threadsafe.
func (j *BashCommand) SetExitCode(e int) {
	j.exitCodeLock.Lock()
	j.exitCode = e
	j.exitCodeLock.Unlock()
}

// ExitCode gets the exitCode from a BashCommand. Should be threadsafe.
func (j *BashCommand) ExitCode() int {
	var retval int
	j.exitCodeLock.Lock()
	retval = j.exitCode
	j.exitCodeLock.Unlock()
	return retval
}

// SetCmdPtr sets the pointer to an exec.Cmd instance for the BashCommand.
// Should be threadsafe.
func (j *BashCommand) SetCmdPtr(p *exec.Cmd) {
	j.cmdPtrLock.Lock()
	j.cmdPtr = p
	j.cmdPtrLock.Unlock()
}

// CmdPtr gets the pointer to an exec.Cmd instance that's associated with
// the BashCommand. Should be threadsafe, but don't don't mutate any state on
// the returned pointer or bad things could happen.
func (j BashCommand) CmdPtr() *exec.Cmd {
	var retval *exec.Cmd
	j.cmdPtrLock.Lock()
	retval = j.cmdPtr
	j.cmdPtrLock.Unlock()
	return retval
}

// SetUUID sets the UUID for a BashCommand.
func (j *BashCommand) SetUUID(uuid string) {
	j.uuidLock.Lock()
	j.uuid = uuid
	j.uuidLock.Unlock()
}

// UUID gets the UUID for a BashCommand.
func (j *BashCommand) UUID() string {
	var retval string
	j.uuidLock.Lock()
	retval = j.uuid
	j.uuidLock.Unlock()
	return retval
}

// MonitorState fires off two goroutines: one that waits for a message on the
// Kill, Completed, or abort channels, and another one that calls Wait() on
// the command that's running. Do not call this method before Start().
func (j *BashCommand) MonitorState() {
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

// Wait blocks until the running command is completed.
func (j *BashCommand) Wait() {
	uuid := j.UUID()
	cmd := j.CmdPtr()
	select {
	case err := <-j.done:
		if j.Killed() { //Command killed
			j.SetExitCode(-100)
			j.completed <- 1
			return
		}
		if err == nil && j.ExitCode() == -9000 { //Command exited normally
			if cmd.ProcessState != nil {
				j.SetExitCode(exitCode(cmd))
			} else {
				j.SetExitCode(0)
			}
		}
		if err != nil { //Command exited badly, but wasn't killed
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

// Start gets the command running. The job will not begin until a command is set,
// the environment is set, and the begin channel receives a message. The best
// way to do all that is with the Prepare() method.
func (j *BashCommand) Start() {
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
func (j *BashCommand) Prepare(command string, environment map[string]string) error {
	err := os.MkdirAll(j.WorkingDir(), 0755)
	if err != nil {
		return err
	}
	go func() {
		j.command <- command
		j.environment <- environment
		j.begin <- 1
		<-j.began
	}()
	return err
}

// Kill kills a command.
func (j *BashCommand) Kill() {
	j.kill <- 1
}
