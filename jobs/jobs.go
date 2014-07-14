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
	"github.com/johnworth/jobrunner/filesystem"

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
	SetWorkingDir(string)
	WorkingDir() string
	UUID() string
	MonitorState()
	Wait() error
	Start() error
	Prepare(interface{}) error
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

// PathExists returns true if the path exists within the jobs working directory.
func (j *Job) PathExists(fpath string) (bool, error) {
	resolvedPath := j.JobPath(fpath)
	exists, err := pathExists(resolvedPath)
	if err != nil {
		return false, err
	}
	return exists, err
}

// JobPath returns the path to the file in the Job's working directory. Does
// not check for existence first.
func (j *Job) JobPath(fpath string) string {
	wdir := j.WorkingDir()
	p := path.Join(wdir, fpath)
	return p
}

// IsDir returns true if the path passed in is a directory and false otherwise.
// If the path doesn't exist it will return false along with an error.
func (j *Job) IsDir(fpath string) (bool, error) {
	exists, err := j.PathExists(fpath)
	if err != nil {
		return false, err
	}
	if !exists {
		return exists, fmt.Errorf("%s does not exist", fpath)
	}
	jpath := j.JobPath(fpath)
	ofile, err := os.Open(jpath)
	if err != nil {
		return false, err
	}
	stat, err := ofile.Stat()
	if err != nil {
		return false, err
	}
	return stat.IsDir(), err
}

// FilePathResolve returns a *os.File representing a file download.
func (j *Job) FilePathResolve(fpath string) (*os.File, error) {
	exists, err := j.PathExists(fpath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("%s does not exist", fpath)
	}
	dircheck, err := j.IsDir(fpath)
	if err != nil {
		return nil, err
	}
	if dircheck {
		return nil, fmt.Errorf("%s is a directory", fpath)
	}
	resolvedPath := j.JobPath(fpath)
	opened, err := os.Open(resolvedPath)
	if err != nil {
		return nil, err
	}
	return opened, err
}

// DirPathResolve returns a *filesystem.DirectoryListing for the path within
// the Job's working directory.
func (j *Job) DirPathResolve(fpath string) (*filesystem.DirectoryListing, error) {
	exists, err := j.PathExists(fpath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("%s does not exist", fpath)
	}
	dircheck, err := j.IsDir(fpath)
	if err != nil {
		return nil, err
	}
	if !dircheck {
		return nil, fmt.Errorf("%s is not a directory", fpath)
	}
	resolvedPath := j.JobPath(fpath)
	listing, err := filesystem.ListDir(resolvedPath)
	if err != nil {
		return nil, err
	}
	return listing, err
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
	client         *docker.Client
	cmd            *exec.Cmd
	cmdLock        *sync.RWMutex
	command        string
	imageID        string
	imageIDLock    *sync.RWMutex
	commandLock    *sync.RWMutex
	done           chan error
	abort          chan int
	environment    []string
	envLock        *sync.RWMutex
	begin          chan int
	began          chan int
	kill           chan int
	killed         bool
	killedLock     *sync.RWMutex
	Output         chan []byte
	exitCode       int
	exitCodeLock   *sync.RWMutex
	completed      chan int
	dockerUUID     string
	dockerUUIDLock *sync.RWMutex
	uuid           string
	uuidLock       *sync.RWMutex
	workingDir     string
	workingDirLock *sync.RWMutex
	repository     string
	registry       string
	tag            string
}

// NewDockerCommand accepts a pointer to a docker Client
func NewDockerCommand(client *docker.Client) *DockerCommand {
	dc := &DockerCommand{
		client:         client,
		cmd:            nil,
		cmdLock:        &sync.RWMutex{},
		command:        "",
		commandLock:    &sync.RWMutex{},
		imageID:        "",
		imageIDLock:    &sync.RWMutex{},
		dockerUUID:     "",
		dockerUUIDLock: &sync.RWMutex{},
		done:           make(chan error),
		abort:          make(chan int),
		environment:    make([]string, 0),
		envLock:        &sync.RWMutex{},
		begin:          make(chan int),
		began:          make(chan int),
		kill:           make(chan int),
		killed:         false,
		killedLock:     &sync.RWMutex{},
		Output:         make(chan []byte),
		exitCode:       -9000,
		exitCodeLock:   &sync.RWMutex{},
		completed:      make(chan int),
		uuid:           "",
		uuidLock:       &sync.RWMutex{},
		workingDir:     "",
		workingDirLock: &sync.RWMutex{},
		repository:     "",
		registry:       "",
		tag:            "",
	}
	return dc
}

// CmdPtr returns the pointer to the exec.Cmd instance that's running docker.
// Use it carefully.
func (d *DockerCommand) CmdPtr() *exec.Cmd {
	d.cmdLock.RLock()
	defer d.cmdLock.RUnlock()
	return d.cmd
}

// SetCmdPtr sets the pointer to the exec.Cmd instance that should be running
// docker.
func (d *DockerCommand) SetCmdPtr(c *exec.Cmd) {
	d.cmdLock.Lock()
	defer d.cmdLock.Unlock()
	d.cmd = c
}

// Command returns the shell command to be run in the container.
func (d *DockerCommand) Command() string {
	d.commandLock.RLock()
	defer d.commandLock.RUnlock()
	return d.command
}

// SetCommand sets the shell command to be run in the container.
func (d *DockerCommand) SetCommand(c string) {
	d.commandLock.Lock()
	defer d.commandLock.Unlock()
	d.command = c
}

// Environment returns the environment variables passed to the container.
func (d *DockerCommand) Environment() []string {
	d.envLock.RLock()
	defer d.envLock.RUnlock()
	return d.environment
}

// SetEnvironment sets the environment variables used in the container.
func (d *DockerCommand) SetEnvironment(e []string) {
	d.envLock.Lock()
	defer d.envLock.Unlock()
	d.environment = e
}

// ImageID returns the docker image ID for this command.
func (d *DockerCommand) ImageID() string {
	d.imageIDLock.RLock()
	defer d.imageIDLock.RUnlock()
	return d.imageID
}

// FormatImageID returns an string image ID from a DockerCommandSettings
// instance. The returned ID will be in the format 'registry/repository:tag'.
func FormatImageID(dcs *DockerCommandSettings) string {
	imageID := dcs.Repository
	if dcs.Registry != "" {
		imageID = fmt.Sprintf("%s/%s", dcs.Registry, imageID)
	}
	if dcs.Tag != "" {
		imageID = fmt.Sprintf("%s:%s", imageID, dcs.Tag)
	}
	return imageID
}

// SetImageID sets the image ID that will be used to start up the container.
func (d *DockerCommand) SetImageID(dcs *DockerCommandSettings) {
	d.imageIDLock.Lock()
	defer d.imageIDLock.Unlock()
	d.imageID = FormatImageID(dcs)
}

// UUID returns the UUID for the DockerCommand as a string. This is NOT the same
// as the UUID returned by Docker when a container is started.
func (d *DockerCommand) UUID() string {
	d.uuidLock.RLock()
	defer d.uuidLock.RUnlock()
	return d.uuid
}

// SetUUID sets the jobrunner assigned UUID for this command.
func (d *DockerCommand) SetUUID(u string) {
	d.uuidLock.Lock()
	defer d.uuidLock.Unlock()
	d.uuid = u
}

// SetWorkingDir sets the working directory for the DockerCommand.
func (d *DockerCommand) SetWorkingDir(w string) {
	d.workingDirLock.Lock()
	defer d.workingDirLock.Unlock()
	d.workingDir = w
}

// WorkingDir returns the DockerCommand's working directory as a string.
func (d *DockerCommand) WorkingDir() string {
	d.workingDirLock.RLock()
	defer d.workingDirLock.RUnlock()
	return d.workingDir
}

// SetExitCode sets the exit code for the DockerCommand.
func (d *DockerCommand) SetExitCode(e int) {
	d.exitCodeLock.Lock()
	defer d.exitCodeLock.Unlock()
	d.exitCode = e
}

// ExitCode returns the exit code for a DockerCommand as an int.
func (d *DockerCommand) ExitCode() int {
	d.exitCodeLock.RLock()
	defer d.exitCodeLock.RUnlock()
	return d.exitCode
}

// MonitorState tracks the current state of the container.
func (d *DockerCommand) MonitorState() {
	return
}

// Wait blocks until the container that DockerCommand tracks finishes executing.
func (d *DockerCommand) Wait() error {
	ptr := d.CmdPtr()
	err := ptr.Wait()
	if err != nil {
		log.Printf(err.Error())
	}
	return err
}

// VolumesSetting returns a []string containing the settings for volumes that get
// passed to docker.
func (d *DockerCommand) VolumesSetting() []string {
	return []string{"-v", fmt.Sprintf("%s:/data", d.WorkingDir())}
}

// Start spins up the DockerCommand. Make sure the image ID and working directory have
// already been set.
func (d *DockerCommand) Start() error {
	args := []string{"run", "-i", "-t"}
	for _, v := range d.VolumesSetting() {
		args = append(args, v)
	}
	for _, e := range d.Environment() {
		if e != "" {
			args = append(args, "--env")
			args = append(args, e)
		}
	}
	args = append(args, "-a")
	args = append(args, "stdout")
	args = append(args, "-a")
	args = append(args, "stderr")
	args = append(args, d.ImageID())
	for _, c := range strings.Split(d.Command(), " ") {
		args = append(args, c)
	}
	//args = append(args, d.Command())
	cmd := exec.Command("docker", args...)
	d.SetCmdPtr(cmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	return err
}

// DockerCommandSettings contains configuration settings for a DockerCommand
// instance. Used for in the Prepare() function.
type DockerCommandSettings struct {
	Repository  string
	Registry    string
	Tag         string
	Command     string
	Environment map[string]string
	WorkingDir  string
}

// Prepare will Pull the Docker image and parse out the environment variables
// into a format that Docker can use. The parameter should be an instance of
// DockerCommandSettings.
func (d *DockerCommand) Prepare(dcs interface{}) error {
	settings := dcs.(DockerCommandSettings)
	d.SetImageID(&settings)
	d.SetWorkingDir(settings.WorkingDir)
	d.SetCommand(settings.Command)
	d.SetEnvironment(formatEnv(settings.Environment))
	d.repository = settings.Repository
	d.registry = settings.Registry
	d.tag = settings.Tag
	pullCmd := exec.Command("docker", "pull", d.ImageID())
	pullCmd.Stdout = os.Stdout
	pullCmd.Stderr = os.Stderr
	err := pullCmd.Run()
	return err
}

// Kill will stop the container.
func (d *DockerCommand) Kill() error {
	d.dockerUUIDLock.RLock()
	defer d.dockerUUIDLock.RUnlock()
	killOpts := docker.KillContainerOptions{
		ID: d.dockerUUID,
	}
	return d.client.KillContainer(killOpts)
}
