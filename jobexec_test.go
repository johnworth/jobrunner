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
