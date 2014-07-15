package jobs

import (
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"testing"

	"code.google.com/p/go-uuid/uuid"
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
	j := NewBashCommand(nil)
	if j.Killed() {
		t.Fail()
	}
}

func TestJobSetKilled(t *testing.T) {
	j := NewBashCommand(nil)
	j.SetKilled(true)
	if !j.Killed() {
		t.Fail()
	}
}

func TestGetExitCode(t *testing.T) {
	j := NewBashCommand(nil)
	if j.ExitCode() != -9000 {
		t.Fail()
	}
}

func TestSetExitCode(t *testing.T) {
	j := NewBashCommand(nil)
	j.SetExitCode(1)
	if j.ExitCode() != 1 {
		t.Fail()
	}
}

func TestCmdPtr(t *testing.T) {
	j := NewBashCommand(nil)
	if j.CmdPtr() != nil {
		t.Fail()
	}
}

func TestSetCmdPtr(t *testing.T) {
	j := NewBashCommand(nil)
	c := exec.Command("echo", "true")
	j.SetCmdPtr(c)
	if j.CmdPtr() != c {
		t.Fail()
	}
}

func TestUUID(t *testing.T) {
	j := NewBashCommand(nil)
	fmt.Println(j.UUID())
	if j.UUID() == "" {
		t.Fail()
	}
}

func TestSetUUID(t *testing.T) {
	j := NewBashCommand(nil)
	u := uuid.New()
	j.SetUUID(u)
	if j.UUID() != u {
		t.Fail()
	}
}

func TestPrepare(t *testing.T) {
	j := NewBashCommand(nil)
	j.SetWorkingDir("/tmp/jobrunnerTestPrepare")
	defer os.RemoveAll("/tmp/jobrunnerTestPrepare")
	var c string
	var e map[string]string
	go func() {
		c = <-j.command
		e = <-j.environment
		<-j.begin
		j.began <- 1
	}()
	bcs := BashCommandSettings{
		Command:     "foobar",
		Environment: map[string]string{"foo": "bar"},
	}
	err := j.Prepare(bcs)
	if err != nil {
		t.Errorf(err.Error())
	}
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
	b := NewBashCommand(nil)
	b.SetWorkingDir("/tmp/testJobStart")
	defer os.RemoveAll("/tmp/testJobStart")
	j.AddCommand(b)
	bcs := BashCommandSettings{
		Command:     "echo foo",
		Environment: map[string]string{},
	}
	b.Prepare(bcs)
	b.Start()
	b.MonitorState()
	if b.CmdPtr() == nil {
		t.Errorf("Start resulted in a nil CmdPtr.")
	}
}

func TestMonitorState1(t *testing.T) {
	j := NewJob()
	b := NewBashCommand(nil)
	b.SetWorkingDir("/tmp/testMonitorState")
	defer os.RemoveAll("/tmp/testMonitorState")
	j.AddCommand(b)
	bcs := BashCommandSettings{
		Command:     "while true; do echo 1; done",
		Environment: map[string]string{},
	}
	b.Prepare(bcs)
	b.Start()
	b.MonitorState()
	b.Kill()
	if !b.Killed() {
		t.Fail()
	}
}

func TestJobWait(t *testing.T) {
	j := NewJob()
	b := NewBashCommand(nil)
	b.SetWorkingDir("/tmp/testJobWait")
	defer os.RemoveAll("/tmp/testJobWait")
	j.AddCommand(b)
	bcs := BashCommandSettings{
		Command:     "echo true",
		Environment: map[string]string{},
	}
	b.Prepare(bcs)
	b.Start()
	b.MonitorState()
	b.Wait()
	if b.Killed() {
		t.Errorf("Command was Killed")
	}
	if b.ExitCode() == -9000 {
		t.Errorf("Exit code wasn't -9000")
	}
}

func TestPathExists(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Errorf(err.Error())
	}
	exists, err := pathExists(wd)
	if err != nil {
		t.Errorf(err.Error())
	}
	if !exists {
		t.Errorf("Path reported as not existing when it should actually exist.")
	}
	exists2, err := pathExists("/asdfasdfasdfa")
	if err != nil {
		t.Errorf(err.Error())
	}
	if exists2 {
		t.Errorf("Path reported as existing when it should not exist.")
	}
}

func TestIsDir(t *testing.T) {
	j := NewJob()
	j.SetWorkingDir("/tmp/")
	err := os.Mkdir("/tmp/dirPathResolve", 0755)
	if err != nil {
		t.Errorf(err.Error())
	}
	defer os.RemoveAll("/tmp/dirPathResolve")
	dir, err := j.IsDir("dirPathResolve")
	if err != nil {
		t.Errorf(err.Error())
	}
	if !dir {
		t.Errorf("Not a directory.")
	}
	opened, err := os.Create("/tmp/dirPathResolve/foo")
	if err != nil {
		t.Errorf(err.Error())
	}
	opened.Write([]byte("this is a test"))
	opened.Sync()
	opened.Close()
	dir2, err := j.IsDir("dirPathResolve/foo")
	if err != nil {
		t.Errorf(err.Error())
	}
	if dir2 {
		t.Errorf("Is a directory and should be a file.")
	}
}

func TestFilePathResolve(t *testing.T) {
	j := NewJob()
	j.SetWorkingDir("/tmp/jobPathResolve")
	err := os.Mkdir("/tmp/jobPathResolve", 0755)
	if err != nil {
		t.Errorf(err.Error())
	}
	defer os.RemoveAll("/tmp/jobPathResolve")
	opened, err := os.Create("/tmp/jobPathResolve/foo")
	if err != nil {
		t.Errorf(err.Error())
	}
	opened.Write([]byte("this is a test"))
	opened.Sync()
	opened.Close()
	_, err = j.FilePathResolve("foo")
	if err != nil {
		t.Errorf(err.Error())
	}
}

func TestDirPathResolve(t *testing.T) {
	j := NewJob()
	j.SetWorkingDir("/tmp/")
	err := os.Mkdir("/tmp/dirPathResolve", 0755)
	if err != nil {
		t.Errorf(err.Error())
	}
	defer os.RemoveAll("/tmp/dirPathResolve")
	_, err = j.DirPathResolve("dirPathResolve")
	if err != nil {
		t.Errorf(err.Error())
	}
}

func TestJobPath(t *testing.T) {
	j := NewJob()
	j.SetWorkingDir("/tmp/")
	jpath := j.JobPath("argle")
	if jpath != "/tmp/argle" {
		t.Errorf("%s is not /tmp/argle", jpath)
	}
}

func TestPathExists2(t *testing.T) {
	j := NewJob()
	j.SetWorkingDir("/tmp/")
	exists0, err := j.PathExists("foo")
	if err != nil {
		t.Errorf(err.Error())
	}
	if exists0 {
		t.Errorf("foo exists.")
	}
}
