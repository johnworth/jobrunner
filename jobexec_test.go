package main

import (
	"io"
	"os/exec"
	"reflect"
	"testing"
)

func TestExitCode(t *testing.T) {
	cmd := exec.Command("echo", "foo")
	cmd.Start()
	cmd.Wait()
	if exitCode(cmd) != 0 {
		t.Fail()
	}
}

func getJobOutputReader() *JobOutputReader {
	jol := &JobOutputListener{
		Listener:   make(chan []byte),
		Latch:      make(chan int),
		Quit:       make(chan int),
		readBuffer: make([]byte, 0),
	}
	return NewJobOutputReader(jol)
}

func TestJobOutputReaderQuitChannel(t *testing.T) {
	r := getJobOutputReader()
	r.Quit()
	if !r.EOF {
		t.Fail()
	}
}

func TestJobOutputReaderListernQuit(t *testing.T) {
	r := getJobOutputReader()
	r.listener.Quit <- 1
	if !r.EOF {
		t.Fail()
	}
}

func TestJobOutputReaderSendBytes(t *testing.T) {
	r := getJobOutputReader()
	testBytes := []byte("testing")
	r.listener.Listener <- testBytes
	if !reflect.DeepEqual(r.accum, testBytes) {
		t.Fail()
	}
	r.Quit()
}

// TestJobOutputReaderRead1 tests a Read with a buffer the same size as the
// test value.
func TestJobOutputReaderRead1(t *testing.T) {
	r := getJobOutputReader()
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

// TestJobOutputReaderRead2 test a Read with a buffer smaller than the test value.
func TestJobOutputReaderRead2(t *testing.T) {
	r := getJobOutputReader()
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

// TestJobOutputReaderRead3 tests a second Read with buffer smaller than the test
// value.
func TestJobOutputReaderRead3(t *testing.T) {
	r := getJobOutputReader()
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

// TestJobOutputReaderRead4 tests a Read after quit is sent when the buffer is
// smaller than the test value.
func TestJobOutputReaderRead4(t *testing.T) {
	r := getJobOutputReader()
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

// TestJobOutputReaderRead5 tests a second Read after quit is sent when the
// buffer is smaller than the test value.
func TestJobOutputReaderRead5(t *testing.T) {
	r := getJobOutputReader()
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

// testJobOutputReaderRead6 test a Read after quit is sent and the buffer is
// larger than the test value.
func TestJobOutputReaderRead6(t *testing.T) {
	r := getJobOutputReader()
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

// TestJobOutputReaderRead7 tests a second Read after quit is sent and the buffer
// is larger than the test value.
func TestJobOutputReaderRead7(t *testing.T) {
	r := getJobOutputReader()
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

func TestJobOutputRegistryListenSetter(t *testing.T) {
	r := NewJobOutputRegistry()
	r.Listen()
	l := r.AddListener()
	foundlistener := false
	for p := range r.Registry {
		if p == l {
			foundlistener = true
		}
	}
	if !foundlistener {
		t.Fail()
	}
}

func TestJobOutputRegistryRemove(t *testing.T) {
	r := NewJobOutputRegistry()
	r.Listen()
	l := r.AddListener()
	r.RemoveListener(l)
	foundlistener := false
	for p := range r.Registry {
		if p == l {
			foundlistener = true
		}
	}
	if foundlistener {
		t.Fail()
	}
}

func TestJobOutputRegistryInput(t *testing.T) {
	r := NewJobOutputRegistry()
	r.Listen()
	l := r.AddListener()
	testbytes := []byte("testing")
	r.Input <- testbytes
	recv := <-l.Listener
	if !reflect.DeepEqual(recv, testbytes) {
		t.Fail()
	}
}

func TestJobOutputRegistryInput2(t *testing.T) {
	r := NewJobOutputRegistry()
	r.Listen()
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

func TestJobSyncerWrite(t *testing.T) {
	s := NewJobSyncer()
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

func TestJobRegistryRegister(t *testing.T) {
	r := NewJobRegistry()
	r.Listen()
	s := NewJobSyncer()
	r.Register("testing", s)
	foundsyncer := false
	for k, v := range r.Registry {
		if k == "testing" && v == s {
			foundsyncer = true
		}
	}
	if !foundsyncer {
		t.Fail()
	}
}

func TestJobRegistryGet(t *testing.T) {
	r := NewJobRegistry()
	r.Listen()
	s := NewJobSyncer()
	r.Register("testing", s)
	get := r.Get("testing")
	if get != s {
		t.Fail()
	}
}

func TestJobRegistryHasKey(t *testing.T) {
	r := NewJobRegistry()
	r.Listen()
	s := NewJobSyncer()
	r.Register("testing", s)
	if !r.HasKey("testing") {
		t.Fail()
	}
	if r.HasKey("testing2") {
		t.Fail()
	}
}

func TestJobRegistryDelete(t *testing.T) {
	r := NewJobRegistry()
	r.Listen()
	s := NewJobSyncer()
	r.Register("testing", s)
	r.Delete(s)
	if r.HasKey("testing") {
		t.Fail()
	}
}

func TestJobRegistryListJobs(t *testing.T) {
	r := NewJobRegistry()
	r.Listen()
	s := NewJobSyncer()
	r.Register("testing", s)
	r.Register("testing2", s)
	r.Register("testing3", s)
	list := r.ListJobs()
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

func TestJobExecutorLaunch(t *testing.T) {
	r := NewJobExecutor()
	jobid := r.Launch("echo foo", make(map[string]string))
	if jobid == "" {
		t.Fail()
	}
}

func TestJobExecutorExecute(t *testing.T) {
	e := NewJobExecutor()
	s := NewJobSyncer()
	ec := make(chan int)
	go func() {
		<-s.Completed
		ec <- s.ExitCode
	}()
	e.Execute(s)
	e.Registry.Register("blippy", s)
	s.Command <- "echo $FOO"
	s.Environment <- map[string]string{"FOO": "BAR"}
	s.Start <- 1
	<-s.Started
	exit := <-ec
	if exit != 0 {
		t.Fail()
	}
}

func TestJobExecutorKill(t *testing.T) {
	e := NewJobExecutor()
	jobid := e.Launch("while true; do echo foo; done", make(map[string]string))
	s := e.Registry.Get(jobid)
	coord := make(chan int)
	go func() {
		<-s.Completed
		coord <- s.ExitCode
	}()
	e.Kill(jobid)
	exit := <-coord
	if exit != -100 {
		t.Errorf("Exit code for the kill command wasn't -100.")
	}
	if !s.Killed {
		t.Errorf("The JobSyncer.Killed field wasn't false.")
	}
	if e.Registry.HasKey(jobid) {
		t.Error("The registry still has a reference to the jobID")
	}
}
