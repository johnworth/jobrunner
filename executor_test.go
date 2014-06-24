package main

import (
	"fmt"
	"io"
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

func getOutputReader() *OutputReader {
	jol := NewOutputListener()
	return NewOutputReader(jol)
}

func TestOutputReaderQuitChannel(t *testing.T) {
	r := getOutputReader()
	r.Quit()
	if !r.eof {
		t.Fail()
	}
}

func TestOutputReaderListernQuit(t *testing.T) {
	r := getOutputReader()
	r.listener.Quit <- 1
	if !r.eof {
		t.Fail()
	}
}

func TestOutputReaderSendBytes(t *testing.T) {
	r := getOutputReader()
	testBytes := []byte("testing")
	r.listener.Listener <- testBytes
	if !reflect.DeepEqual(r.accum, testBytes) {
		t.Fail()
	}
	r.Quit()
}

// TestOutputReaderRead1 tests a Read with a buffer the same size as the
// test value.
func TestOutputReaderRead1(t *testing.T) {
	r := getOutputReader()
	testBytes := []byte("testing")
	r.listener.Listener <- testBytes
	buf := make([]byte, len(testBytes))
	read, err := r.Read(buf)
	if err != nil {
		t.Fail()
	}
	if read != len(testBytes) {
		t.Fail()
	}
	if !reflect.DeepEqual(buf, testBytes) {
		t.Fail()
	}
	r.Quit()
}

// TestOutputReaderRead2 test a Read with a buffer smaller than the test value.
func TestOutputReaderRead2(t *testing.T) {
	r := getOutputReader()
	testBytes := []byte("testing")
	r.listener.Listener <- testBytes
	buf := make([]byte, len(testBytes)-2)
	read, err := r.Read(buf)
	if err != nil {
		t.Fail()
	}
	if read != (len(testBytes) - 2) {
		t.Fail()
	}
	if !reflect.DeepEqual(buf, []byte("testi")) {
		t.Fail()
	}
	r.Quit()
}

// TestOutputReaderRead3 tests a second Read with buffer smaller than the test
// value.
func TestOutputReaderRead3(t *testing.T) {
	r := getOutputReader()
	testBytes := []byte("testing")
	r.listener.Listener <- testBytes
	buf := make([]byte, len(testBytes)-2)
	buf2 := make([]byte, len(testBytes)-2)
	read, err := r.Read(buf)
	read, err = r.Read(buf2)
	//EOF isn't set because a quit hasn't been sent to the reader.
	if err != nil {
		t.Fail()
	}
	if read != 2 {
		t.Fail()
	}
	if string(buf2[:read]) != "ng" {
		t.Fail()
	}
	r.Quit()
}

// TestOutputReaderRead4 tests a Read after quit is sent when the buffer is
// smaller than the test value.
func TestOutputReaderRead4(t *testing.T) {
	r := getOutputReader()
	testBytes := []byte("testing")
	r.listener.Listener <- testBytes
	buf := make([]byte, len(testBytes)-2)
	r.Quit()
	read, err := r.Read(buf)
	//EOF isn't set because there's still info left in the buffer.
	if err != nil {
		t.Fail()
	}
	if read != 5 {
		t.Fail()
	}
	if string(buf[:read]) != "testi" {
		t.Fail()
	}
}

// TestOutputReaderRead5 tests a second Read after quit is sent when the
// buffer is smaller than the test value.
func TestOutputReaderRead5(t *testing.T) {
	r := getOutputReader()
	testBytes := []byte("testing")
	r.listener.Listener <- testBytes
	buf := make([]byte, len(testBytes)-2)
	r.Quit()
	read, err := r.Read(buf)
	read, err = r.Read(buf)
	if err != io.EOF {
		t.Fail()
	}
	if read != 2 {
		t.Fail()
	}
	if string(buf[:read]) != "ng" {
		t.Fail()
	}
}

// testOutputReaderRead6 test a Read after quit is sent and the buffer is
// larger than the test value.
func TestOutputReaderRead6(t *testing.T) {
	r := getOutputReader()
	testBytes := []byte("testing")
	r.listener.Listener <- testBytes
	buf := make([]byte, len(testBytes)+2)
	r.Quit()
	read, err := r.Read(buf)
	if err != io.EOF {
		t.Fail()
	}
	if read != len(testBytes) {
		t.Fail()
	}
	if string(buf[:read]) != string(testBytes[:]) {
		t.Fail()
	}
}

// TestOutputReaderRead7 tests a second Read after quit is sent and the buffer
// is larger than the test value.
func TestOutputReaderRead7(t *testing.T) {
	r := getOutputReader()
	testBytes := []byte("testing")
	r.listener.Listener <- testBytes
	buf := make([]byte, len(testBytes)+2)
	r.Quit()
	read, err := r.Read(buf)
	read, err = r.Read(buf)
	if err != io.EOF {
		t.Fail()
	}
	if read != 0 {
		t.Fail()
	}
}

func TestOutputRegistryListenSetter(t *testing.T) {
	r := NewOutputRegistry()
	l := r.AddListener()
	if !r.HasKey(l) {
		t.Fail()
	}
}

func TestOutputRegistryRemove(t *testing.T) {
	r := NewOutputRegistry()
	l := r.AddListener()
	r.RemoveListener(l)
	if r.HasKey(l) {
		t.Fail()
	}
}

func TestOutputRegistryInput(t *testing.T) {
	r := NewOutputRegistry()
	l := r.AddListener()
	testbytes := []byte("testing")
	r.Input <- testbytes
	recv := <-l.Listener
	if !reflect.DeepEqual(recv, testbytes) {
		t.Fail()
	}
}

func TestOutputRegistryInput2(t *testing.T) {
	r := NewOutputRegistry()
	l1 := r.AddListener()
	l2 := r.AddListener()
	testbytes := []byte("testing")
	r.Input <- testbytes
	var recv1 []byte
	var recv2 []byte
	for {
		select {
		case recv1 = <-l1.Listener:
			close(l1.Listener)
			l1.Listener = nil
		case recv2 = <-l2.Listener:
			close(l2.Listener)
			l2.Listener = nil
		}
		if l1.Listener == nil && l2.Listener == nil {
			break
		}
	}
	if !reflect.DeepEqual(recv1, testbytes) {
		t.Fail()
	}
	if !reflect.DeepEqual(recv2, testbytes) {
		t.Fail()
	}

}

func TestJobWrite(t *testing.T) {
	s := NewJob()
	r := s.OutputRegistry
	l1 := r.AddListener()
	l2 := r.AddListener()
	testbytes := []byte("testing")
	s.Write(testbytes)
	var recv1 []byte
	var recv2 []byte
	for {
		select {
		case recv1 = <-l1.Listener:
			close(l1.Listener)
			l1.Listener = nil
		case recv2 = <-l2.Listener:
			close(l2.Listener)
			l2.Listener = nil
		}
		if l1.Listener == nil && l2.Listener == nil {
			break
		}
	}
	if !reflect.DeepEqual(recv1, testbytes) {
		t.Fail()
	}
	if !reflect.DeepEqual(recv2, testbytes) {
		t.Fail()
	}
}

func TestJobKilled(t *testing.T) {
	j := NewJob()
	if j.Killed() {
		t.Fail()
	}
}

func TestJobSetKilled(t *testing.T) {
	j := NewJob()
	j.SetKilled(true)
	if !j.Killed() {
		t.Fail()
	}
}

func TestGetExitCode(t *testing.T) {
	j := NewJob()
	if j.ExitCode() != -9000 {
		t.Fail()
	}
}

func TestSetExitCode(t *testing.T) {
	j := NewJob()
	j.SetExitCode(1)
	if j.ExitCode() != 1 {
		t.Fail()
	}
}

func TestCmdPtr(t *testing.T) {
	j := NewJob()
	if j.CmdPtr() != nil {
		t.Fail()
	}
}

func TestSetCmdPtr(t *testing.T) {
	j := NewJob()
	c := exec.Command("echo", "true")
	j.SetCmdPtr(c)
	if j.CmdPtr() != c {
		t.Fail()
	}
}

func TestUUID(t *testing.T) {
	j := NewJob()
	if j.UUID() != "" {
		t.Fail()
	}
}

func TestSetUUID(t *testing.T) {
	j := NewJob()
	u := uuid.New()
	j.SetUUID(u)
	if j.UUID() != u {
		t.Fail()
	}
}

func TestPrepare(t *testing.T) {
	j := NewJob()
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
	j.Prepare("echo foo", map[string]string{})
	j.Start()
	j.MonitorState()
	if j.CmdPtr() == nil {
		t.Errorf("Start resulted in a nil CmdPtr.")
	}
}

func TestMonitorState1(t *testing.T) {
	j := NewJob()
	j.Prepare("while true; do echo 1; done", map[string]string{})
	j.Start()
	j.MonitorState()
	j.Kill()
	if !j.Killed() {
		t.Fail()
	}
}

func TestJobWait(t *testing.T) {
	j := NewJob()
	j.Prepare("echo true", map[string]string{})
	j.Start()
	j.MonitorState()
	j.Wait()
	if j.Killed() {
		t.Fail()
	}
	if j.ExitCode() == -9000 {
		t.Fail()
	}
}

func TestRegistryRegister(t *testing.T) {
	r := NewRegistry()
	s := NewJob()
	r.Register("testing", s)
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

func TestExecutorLaunch(t *testing.T) {
	r := NewExecutor()
	jobid := r.Launch("echo foo", make(map[string]string))
	if jobid == "" {
		t.Fail()
	}
}

func TestExecutorExecute(t *testing.T) {
	e := NewExecutor()
	s := NewJob()
	ec := make(chan int)
	go func() {
		<-s.completed
		ec <- s.ExitCode()
	}()
	s.SetUUID("blippy")
	s.Prepare("echo $FOO", map[string]string{"FOO": "BAR"})
	e.Execute(s)
	e.Registry.Register("blippy", s)
	exit := <-ec
	fmt.Println(exit)
	if exit != 0 {
		t.Fail()
	}
}

func TestExecutorKill(t *testing.T) {
	e := NewExecutor()
	jobid := e.Launch("while true; do echo foo; done", make(map[string]string))
	s := e.Registry.Get(jobid)
	coord := make(chan int)
	go func() {
		<-s.completed
		coord <- s.ExitCode()
	}()
	e.Kill(jobid)
	exit := <-coord
	if exit != -100 {
		t.Errorf("Exit code for the kill command wasn't -100.")
	}
	if !s.Killed() {
		t.Errorf("The Job.Killed field wasn't false.")
	}
	if e.Registry.HasKey(jobid) {
		t.Error("The registry still has a reference to the jobID")
	}
}
