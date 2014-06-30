package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"reflect"

	"code.google.com/p/go-uuid/uuid"

	"testing"
	"time"
)

func TestExitCode(t *testing.T) {
	cmd := exec.Command("echo", "foo")
	cmd.Start()
	cmd.Wait()
	if exitCode(cmd) != 0 {
		t.Fail()
	}
}

func TestJobKilled(t *testing.T) {
	j := NewBashCommand()
	if j.Killed() {
		t.Fail()
	}
}

func TestJobSetKilled(t *testing.T) {
	j := NewBashCommand()
	j.SetKilled(true)
	if !j.Killed() {
		t.Fail()
	}
}

func TestGetExitCode(t *testing.T) {
	j := NewBashCommand()
	if j.ExitCode() != -9000 {
		t.Fail()
	}
}

func TestSetExitCode(t *testing.T) {
	j := NewBashCommand()
	j.SetExitCode(1)
	if j.ExitCode() != 1 {
		t.Fail()
	}
}

func TestCmdPtr(t *testing.T) {
	j := NewBashCommand()
	if j.CmdPtr() != nil {
		t.Fail()
	}
}

func TestSetCmdPtr(t *testing.T) {
	j := NewBashCommand()
	c := exec.Command("echo", "true")
	j.SetCmdPtr(c)
	if j.CmdPtr() != c {
		t.Fail()
	}
}

func TestUUID(t *testing.T) {
	j := NewBashCommand()
	if j.UUID() != "" {
		t.Fail()
	}
}

func TestSetUUID(t *testing.T) {
	j := NewBashCommand()
	u := uuid.New()
	j.SetUUID(u)
	if j.UUID() != u {
		t.Fail()
	}
}

func TestPrepare(t *testing.T) {
	j := NewBashCommand()
	var c string
	var e map[string]string
	go func() {
		c = <-j.command
		e = <-j.environment
		<-j.begin
		j.began <- 1
	}()
	j.Prepare("foobar", map[string]string{"foo": "bar"})
	<-j.began
	if c != "foobar" {
		t.Errorf("The command was not set to %s.", "foobar")
	}
	if !reflect.DeepEqual(e, map[string]string{"foo": "bar"}) {
		t.Errorf("The environment was not set to {\"foo\":\"bar\"}.")
	}
}

func TestJobStart(t *testing.T) {
	j := NewJob()
	b := NewBashCommand()
	j.AddCommand(b)
	b.Prepare("echo foo", map[string]string{})
	b.Start()
	b.MonitorState()
	if b.CmdPtr() == nil {
		t.Errorf("Start resulted in a nil CmdPtr.")
	}
}

func TestMonitorState1(t *testing.T) {
	j := NewJob()
	b := NewBashCommand()
	j.AddCommand(b)
	b.Prepare("while true; do echo 1; done", map[string]string{})
	b.Start()
	b.MonitorState()
	b.Kill()
	if !b.Killed() {
		t.Fail()
	}
}

func TestJobWait(t *testing.T) {
	j := NewJob()
	b := NewBashCommand()
	j.AddCommand(b)
	b.Prepare("echo true", map[string]string{})
	b.Start()
	b.MonitorState()
	b.Wait()
	if b.Killed() {
		t.Fail()
	}
	if b.ExitCode() == -9000 {
		t.Fail()
	}
}

func TestRegistryRegister(t *testing.T) {
	j := NewJob()
	r := NewRegistry()
	j.SetUUID("testing")
	r.Register("testing", j)
	foundjob := r.HasKey("testing")
	if !foundjob {
		t.Fail()
	}
}

func TestRegistryGet(t *testing.T) {
	r := NewRegistry()
	s := NewJob()
	r.Register("testing", s)
	get := r.Get("testing")
	if get != s {
		t.Fail()
	}
}

func TestRegistryHasKey(t *testing.T) {
	r := NewRegistry()
	s := NewJob()
	r.Register("testing", s)
	if !r.HasKey("testing") {
		t.Fail()
	}
	if r.HasKey("testing2") {
		t.Fail()
	}
}

func TestRegistryDelete(t *testing.T) {
	r := NewRegistry()
	s := NewJob()
	r.Register("testing", s)
	r.Delete("testing")
	if r.HasKey("testing") {
		t.Fail()
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	s := NewJob()
	r.Register("testing", s)
	r.Register("testing2", s)
	r.Register("testing3", s)
	list := r.List()
	found1 := false
	found2 := false
	found3 := false
	for _, v := range list {
		if v == "testing" {
			found1 = true
		}
		if v == "testing2" {
			found2 = true
		}
		if v == "testing3" {
			found3 = true
		}
	}

	if !found1 || !found2 || !found3 {
		t.Fail()
	}
}

func TestExecutorExecute(t *testing.T) {
	var start StartMsg
	jobsString := "{\"Commands\":[{\"CommandLine\":\"echo foo\", \"Environment\":{}}]}"
	json.Unmarshal([]byte(jobsString), &start)
	e := NewExecutor()
	fmt.Println(len(start.Commands))
	id, commandIDs := e.Execute(&start.Commands)
	if id == "" {
		t.Errorf("A Job ID was not returned.")
	}
	if len(commandIDs) != 1 {
		t.Errorf("Number of command IDs returned was not 1, it was %d.", len(commandIDs))
	}
}

func TestExecutorKill(t *testing.T) {
	var start StartMsg
	jobString := "{\"Commands\":[{\"CommandLine\":\"while true; do echo foo; done\", \"Environment\":{}}]}"
	json.Unmarshal([]byte(jobString), &start)
	e := NewExecutor()
	jobid, _ := e.Execute(&start.Commands)
	s := e.Registry.Get(jobid)
	coord := make(chan int)
	go func() {
		for {
			if s.Completed() {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		coord <- 1
	}()
	e.Kill(jobid)
	<-coord
	cmds := s.Commands()
	lastJob := cmds[len(cmds)-1].(*BashCommand) //type JobCommand
	exit := lastJob.ExitCode()
	if exit != -100 {
		t.Errorf("Exit code for the kill command wasn't -100.")
	}
	if !lastJob.Killed() {
		t.Errorf("The Job.Killed field wasn't false.")
	}
	if e.Registry.HasKey(jobid) {
		t.Error("The registry still has a reference to the jobID")
	}
}
