package executor

import (
	"encoding/json"
	"fmt"

	"github.com/johnworth/jobrunner/jobs"

	"testing"
	"time"
)

func TestRegistryRegister(t *testing.T) {
	j := jobs.NewJob()
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
	s := jobs.NewJob()
	r.Register("testing", s)
	get := r.Get("testing")
	if get != s {
		t.Fail()
	}
}

func TestRegistryHasKey(t *testing.T) {
	r := NewRegistry()
	s := jobs.NewJob()
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
	s := jobs.NewJob()
	r.Register("testing", s)
	r.Delete("testing")
	if r.HasKey("testing") {
		t.Fail()
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	s := jobs.NewJob()
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
	lastJob := cmds[len(cmds)-1].(*jobs.BashCommand) //type JobCommand
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
