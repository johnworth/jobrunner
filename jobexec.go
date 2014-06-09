package main

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// exitCode returns the integer exit code of a command. Start() and Wait()
// should have already been called on the Command.
func exitCode(cmd *exec.Cmd) int {
	return cmd.ProcessState.Sys().(syscall.WaitStatus).ExitStatus()
}

// Listener contains a channel that reads byte arrays.
type Listener struct {
	Listen chan []byte
}

// JobSyncer contains sync channels and maintains a registry of channels.
type JobSyncer struct {
	Command  chan string
	Start    chan int
	Output   chan []byte
	ExitCode chan int
	Registry []Listener
}

// Write sends the []byte array passed out on RoutineWriter's OutChannel
func (j *JobSyncer) Write(p []byte) (n int, err error) {
	fmt.Println(string(p[:]))
	for i := range j.Registry {
		j.Registry[i].Listen <- p
	}
	return len(p), nil
}

// A executor executes a job
type executor func(*JobSyncer)

// jobExecutor returns a function and a integer quit channel. The function
// accepts in a string channel and returns. The string channel is used get a
// command-line that will be executed in the function. The quit channel sends
// out an int representing the job's exit code. If a -1000 is returned as the exit
// code, then the job failed to start. If a -2000 is returned, then the job
// failed when Wait() was called. The parameter is a string channel that takes
// the command line for the job.
func jobExecutor() executor {
	return func(j *JobSyncer) {
		cmdString := <-j.Command
		cmdSections := strings.Split(cmdString, " ")
		cmdName := cmdSections[0]
		cmdArgs := strings.Join(cmdSections[1:], " ")
		cmd := exec.Command(cmdName, cmdArgs)
		cmd.Stdout = j
		cmd.Stderr = j
		<-j.Start
		fmt.Println("Starting")
		err := cmd.Start()
		if err != nil {
			fmt.Println(err)
			j.ExitCode <- -1000
			return
		}
		err = cmd.Wait()
		if err != nil {
			fmt.Println(err)
			j.ExitCode <- -2000
			return
		}
		j.ExitCode <- exitCode(cmd)
	}
}
