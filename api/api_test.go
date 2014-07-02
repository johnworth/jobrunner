package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/johnworth/jobrunner/config"
	"github.com/johnworth/jobrunner/executor"
)

func init() {
	config.Configure()
}

func TestStart(t *testing.T) {
	h := NewAPIHandlers()
	reqBody := strings.NewReader("{\"Commands\" : [{\"CommandLine\":\"echo foo\", \"Environment\":{}}]}")
	req, err := http.NewRequest("POST", "http://localhost:8080/", reqBody)
	if err != nil {
		t.Fail()
	}
	rec := httptest.NewRecorder()
	h.Start(rec, req)
	if rec.Code != 200 {
		t.Errorf("Status code was not 200.")
	}
	dec := json.NewDecoder(rec.Body)
	var msg executor.IDMsg
	if err := dec.Decode(&msg); err != nil {
		t.Errorf("Failed to decode response body.")
	}
	if msg.JobID == "" {
		t.Errorf("Response body is missing the ID field.")
	}
	job := h.Executor.Registry.Get(msg.JobID)
	defer os.RemoveAll(job.WorkingDir())
	if !reflect.DeepEqual(reflect.TypeOf(msg.JobID), reflect.TypeOf("")) {
		t.Errorf("Response Body is not a string.")
	}
}

func slicesEquivalent(s1 []string, s2 []string) bool {
	if len(s1) != len(s2) {
		return true
	}
	for i := range s1 {
		foundit := false
		for j := range s2 {
			if s1[i] == s2[j] {
				foundit = true
			}
		}
		if !foundit {
			return false
		}
	}
	return true
}

func TestList(t *testing.T) {
	h := NewAPIHandlers()
	reqBody := strings.NewReader("{\"Commands\" : [{\"CommandLine\":\"while true; do echo $FOO; done\", \"Environment\":{}}]}")
	reqBody2 := strings.NewReader("{\"Commands\" : [{\"CommandLine\":\"while true; do echo $FOO; done\", \"Environment\":{}}]}")
	startReq, err := http.NewRequest("POST", "http://localhost:8080/", reqBody)
	if err != nil {
		t.Fail()
	}
	startReq2, err := http.NewRequest("POST", "http://localhost:8080", reqBody2)
	if err != nil {
		t.Fail()
	}
	rec1 := httptest.NewRecorder()
	rec2 := httptest.NewRecorder()
	h.Start(rec1, startReq)
	h.Start(rec2, startReq2)
	dec1 := json.NewDecoder(rec1.Body)
	dec2 := json.NewDecoder(rec2.Body)
	var msg1 executor.IDMsg
	if err := dec1.Decode(&msg1); err != nil {
		t.Errorf("Failed to decode response body.")
	}
	job1 := h.Executor.Registry.Get(msg1.JobID)
	defer h.Executor.Kill(job1)
	defer os.RemoveAll(job1.WorkingDir())
	var msg2 executor.IDMsg
	if err := dec2.Decode(&msg2); err != nil {
		t.Errorf("Failed to decode response body.")
	}
	job2 := h.Executor.Registry.Get(msg2.JobID)
	defer h.Executor.Kill(job2)
	defer os.RemoveAll(job2.WorkingDir())

	var IDs []string
	IDs = append(IDs, msg1.JobID)
	IDs = append(IDs, msg2.JobID)
	var listmsg ListResponse
	listReq, err := http.NewRequest("GET", "http://localhost:8080/", nil)
	if err != nil {
		t.Fail()
	}
	rec3 := httptest.NewRecorder()
	h.List(rec3, listReq)
	dec3 := json.NewDecoder(rec3.Body)
	if err := dec3.Decode(&listmsg); err != nil {
		t.Errorf("Failed to decode list message response body.")
	}
	if listmsg.IDs == nil {
		t.Errorf("No IDs were returned when listing jobs.")
	}
	if !slicesEquivalent(listmsg.IDs, IDs) {
		t.Errorf("IDs returned by List didn't match.")
	}
}
