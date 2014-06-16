package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestStartJob(t *testing.T) {
	h := NewAPIHandlers()
	reqBody := strings.NewReader("{\"CommandLine\":\"echo foo\", \"Environment\":{}}")
	req, err := http.NewRequest("POST", "http://localhost:8080/", reqBody)
	if err != nil {
		t.Fail()
	}
	rec := httptest.NewRecorder()
	h.StartJob(rec, req)
	if rec.Code != 200 {
		t.Errorf("Status code was not 200.")
	}
	dec := json.NewDecoder(rec.Body)
	var msg JobIDMsg
	if err := dec.Decode(&msg); err != nil {
		t.Errorf("Failed to decode response body.")
	}
	if msg.JobID == "" {
		t.Errorf("Response body is missing the JobID field.")
	}
	if !reflect.DeepEqual(reflect.TypeOf(msg.JobID), reflect.TypeOf("")) {
		t.Errorf("Response Body is not a string.")
	}
}

func TestListJobs(t *testing.T) {
	h := NewAPIHandlers()
	reqBody := strings.NewReader("{\"CommandLine\":\"while true; do echo $FOO; done\", \"Environment\":{}}")
	reqBody2 := strings.NewReader("{\"CommandLine\":\"while true; do echo $FOO; done\", \"Environment\":{}}")
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
	h.StartJob(rec1, startReq)
	h.StartJob(rec2, startReq2)
	dec1 := json.NewDecoder(rec1.Body)
	dec2 := json.NewDecoder(rec2.Body)
	var msg1 JobIDMsg
	if err := dec1.Decode(&msg1); err != nil {
		t.Errorf("Failed to decode response body.")
	}
	var msg2 JobIDMsg
	if err := dec2.Decode(&msg2); err != nil {
		t.Errorf("Failed to decode response body.")
	}
	var jobids []string
	jobids = append(jobids, msg1.JobID)
	jobids = append(jobids, msg2.JobID)
	var listmsg ListJobsResponse
	listReq, err := http.NewRequest("GET", "http://localhost:8080/", nil)
	if err != nil {
		t.Fail()
	}
	rec3 := httptest.NewRecorder()
	h.ListJobs(rec3, listReq)
	dec3 := json.NewDecoder(rec3.Body)
	if err := dec3.Decode(&listmsg); err != nil {
		t.Errorf("Failed to decode list message response body.")
	}
	if listmsg.JobIDs == nil {
		t.Errorf("No JobIDs were returned when listing jobs.")
	}
	if !reflect.DeepEqual(listmsg.JobIDs, jobids) {
		t.Errorf("Job ID lists didn't match.")
	}
}
