package jobs

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"syscall"

	"github.com/fsouza/go-dockerclient"
	"github.com/johnworth/jobrunner/config"
	"github.com/johnworth/jobrunner/jsonify"

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
	//SetUUID(uuid string)
	//SetWorkingDir(string)
	Setup(string) error
	//SetJSONCmd(*jsonify.JSONCmd)
	//JSONCmd() *jsonify.JSONCmd
	//WorkingDir() string
	//UUID() string
	MonitorState()
	Wait() error
	Start() error
	//Prepare(interface{}) error
	Kill() error
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
		err := c.Start()
		if err != nil {
			log.Println(err.Error())
			break
		}
		c.MonitorState()
		err = c.Wait()
		if err != nil {
			log.Println(err.Error())
			break
		}
	}
	j.SetCompleted(true)
}

// BashCommand contains all of the state associated with a command run through
// bash.
type BashCommand struct {
	command        chan string
	jsonCmd        *jsonify.JSONCmd
	jsonCmdLock    *sync.RWMutex
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
func NewBashCommand(jcmd *jsonify.JSONCmd) *BashCommand {
	newUUID := uuid.New()
	j := &BashCommand{
		command:        make(chan string),
		jsonCmd:        jcmd,
		jsonCmdLock:    &sync.RWMutex{},
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
		cmdPtrLock:     &sync.Mutex{},
		uuid:           newUUID,
		uuidLock:       &sync.Mutex{},
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

// SetJSONCmd sets the jsonify.JSONCmd associated with this Command.
func (j *BashCommand) SetJSONCmd(c *jsonify.JSONCmd) {
	j.jsonCmdLock.Lock()
	defer j.jsonCmdLock.Unlock()
	j.jsonCmd = c
}

// JSONCmd returns the jsonify.JSONCmd associated with this Command.
func (j *BashCommand) JSONCmd() *jsonify.JSONCmd {
	j.jsonCmdLock.RLock()
	defer j.jsonCmdLock.RUnlock()
	return j.jsonCmd
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

// Setup creates the working directory and calls Prepare()
func (j *BashCommand) Setup(workingDir string) error {
	c := j.JSONCmd()
	if c.WorkingDir != "" {
		j.SetWorkingDir(path.Join(workingDir, c.WorkingDir))
	} else {
		j.SetWorkingDir(workingDir)
	}
	settings := BashCommandSettings{
		Command:     c.CommandLine,
		Environment: c.Environment,
	}
	err := j.Prepare(settings)
	if err != nil {
		return err
	}
	return err
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
func (j *BashCommand) Wait() error {
	uuid := j.UUID()
	cmd := j.CmdPtr()
	select {
	case err := <-j.done:
		if j.Killed() { //Command killed
			j.SetExitCode(-100)
			j.completed <- 1
			return nil
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
		return nil
	}
}

// Start gets the command running. The job will not begin until a command is set,
// the environment is set, and the begin channel receives a message. The best
// way to do all that is with the Prepare() method.
func (j *BashCommand) Start() error {
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
	} else {
		log.Printf("Started job %s.", j.UUID())
	}
	return err
}

// BashCommandSettings contains settings needed by a BashCommand instance for its
// Prepare function.
type BashCommandSettings struct {
	Command     string
	Environment map[string]string
}

// Prepare allows the caller to set the command and environment for the job.
// Call this before or after Start(). It doesn't matter when, but it must be
// called. The parameter should be an instance of BashCommandSettings.
func (j *BashCommand) Prepare(bcs interface{}) error {
	settings := bcs.(BashCommandSettings)
	command := settings.Command
	environment := settings.Environment
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
func (j *BashCommand) Kill() error {
	j.kill <- 1
	return nil
}

// DockerCommand represents a command that runs in a Docker instance. The UUID
// returned is NOT the same UUID assigned by docker at start since creating
// a DockerCommand is not the same as running it.
type DockerCommand struct {
	Client      *docker.Client
	JSONCmd     *jsonify.JSONCmd
	Cmd         *exec.Cmd
	Command     string
	ImageID     string
	done        chan error
	abort       chan int
	Environment []string
	begin       chan int
	began       chan int
	kill        chan int
	Killed      bool
	Output      chan []byte
	ExitCode    int
	Completed   chan int
	DockerUUID  string
	UUID        string
	WorkingDir  string
	Repository  string
	Registry    string
	Tag         string
}

// NewDockerCommand accepts a pointer to a docker Client
func NewDockerCommand(client *docker.Client, jcmd *jsonify.JSONCmd) *DockerCommand {
	uuid := uuid.New()
	dc := &DockerCommand{
		Client:      client,
		JSONCmd:     jcmd,
		Command:     "",
		ImageID:     "",
		DockerUUID:  "",
		done:        make(chan error),
		abort:       make(chan int),
		Environment: make([]string, 0),
		begin:       make(chan int),
		began:       make(chan int),
		kill:        make(chan int),
		Killed:      false,
		Output:      make(chan []byte),
		ExitCode:    -9000,
		Completed:   make(chan int),
		UUID:        uuid,
		WorkingDir:  "",
		Repository:  "",
		Registry:    "",
		Tag:         "",
	}
	return dc
}

// FormatImageID returns an string image ID from a DockerCommandSettings
// instance. The returned ID will be in the format 'registry/repository:tag'.
func FormatImageID(repo string, registry string, tag string) string {
	imageID := repo
	if registry != "" {
		imageID = fmt.Sprintf("%s/%s", registry, imageID)
	}
	if tag != "" {
		imageID = fmt.Sprintf("%s:%s", imageID, tag)
	}
	return imageID
}

// MonitorState tracks the current state of the container.
func (d *DockerCommand) MonitorState() {
	return
}

// Wait blocks until the container that DockerCommand tracks finishes executing.
func (d *DockerCommand) Wait() error {
	err := d.Cmd.Wait()
	if err != nil {
		log.Printf(err.Error())
	}
	return err
}

// VolumesSetting returns a []string containing the settings for volumes that get
// passed to docker.
func (d *DockerCommand) VolumesSetting() []string {
	return []string{"-v", fmt.Sprintf("%s:/data", d.WorkingDir)}
}

// Start spins up the DockerCommand. Make sure the image ID and working directory have
// already been set.
func (d *DockerCommand) Start() error {
	args := []string{"run", "-i", "-t"}
	for _, v := range d.VolumesSetting() {
		args = append(args, v)
	}
	for _, e := range d.Environment {
		if e != "" {
			args = append(args, "--env")
			args = append(args, e)
		}
	}
	args = append(args, "-a")
	args = append(args, "stdout")
	args = append(args, "-a")
	args = append(args, "stderr")
	args = append(args, d.ImageID)
	for _, c := range strings.Split(d.Command, " ") {
		args = append(args, c)
	}
	stdoutFile, err := os.Create(path.Join(d.WorkingDir, d.UUID+".stdout"))
	if err != nil {
		log.Printf(err.Error())
		return err
	}
	stderrFile, err := os.Create(path.Join(d.WorkingDir, d.UUID+".stderr"))
	if err != nil {
		log.Printf(err.Error())
		return err
	}
	d.Cmd = exec.Command("docker", args...)
	d.Cmd.Stdout = stdoutFile
	d.Cmd.Stderr = stderrFile
	err = d.Cmd.Start()
	return err
}

// Setup sets the working directory and calls Prepare()
func (d *DockerCommand) Setup(workingDir string) error {
	if d.JSONCmd.WorkingDir != "" {
		d.WorkingDir = path.Join(workingDir, d.JSONCmd.WorkingDir)
	} else {
		d.WorkingDir = workingDir
	}
	d.ImageID = FormatImageID(
		d.JSONCmd.Repository,
		d.JSONCmd.Registry,
		d.JSONCmd.Tag,
	)
	d.Command = d.JSONCmd.CommandLine
	d.Environment = formatEnv(d.JSONCmd.Environment)
	d.Repository = d.JSONCmd.Repository
	d.Registry = d.JSONCmd.Registry
	d.Tag = d.JSONCmd.Tag
	pullStdout, err := os.Create(path.Join(d.WorkingDir, "docker-pull.stdout"))
	if err != nil {
		log.Println(err.Error())
		return err
	}
	pullStderr, err := os.Create(path.Join(d.WorkingDir, "docker-pull.stderr"))
	if err != nil {
		log.Println(err.Error())
		return err
	}
	pullCmd := exec.Command("docker", "pull", d.ImageID)
	pullCmd.Stdout = pullStdout
	pullCmd.Stderr = pullStderr
	err = pullCmd.Run()
	return err
}

// Kill will stop the container.
func (d *DockerCommand) Kill() error {
	killStdout, err := os.Create(path.Join(d.WorkingDir, "docker-kill.stdout"))
	if err != nil {
		return nil
	}
	killStderr, err := os.Create(path.Join(d.WorkingDir, "docker-kill.stderr"))
	if err != nil {
		return nil
	}
	killCmd := exec.Command("docker", "kill", d.DockerUUID)
	killCmd.Stdout = killStdout
	killCmd.Stderr = killStderr
	err = killCmd.Run()
	return err
}
