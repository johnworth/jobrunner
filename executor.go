package main

import (
	"fmt"
	"log"
	"os/exec"
	"syscall"

	"code.google.com/p/go-uuid/uuid"
)

// exitCode returns the integer exit code of a command. Start() and Wait()
// should have already been called on the Command.
func exitCode(cmd *exec.Cmd) int {
	return cmd.ProcessState.Sys().(syscall.WaitStatus).ExitStatus()
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
	job := NewBashJob()
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
func (e *Executor) Execute(j *BashJob) {
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
