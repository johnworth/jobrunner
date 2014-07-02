package config

import (
	"fmt"
	"os"
)

// Config contains the configuration values for jobrunner.
type Config chan configMsg

var config Config

// ConfigValues contains the configuration values.
type ConfigValues struct {
	Host    string //The hostname:port that jobrunner should use.
	BaseDir string //The base working directory for the jobs.
}

type configAction int

const (
	get configAction = iota
	set
)

type configMsg struct {
	action configAction
	value  ConfigValues
	result chan interface{}
}

// Configure gathers the configuration values for jobrunner and returns a
// pointer to a filled out Config instance.
func Configure() error {
	var err error
	config = make(Config)
	port := os.Getenv("JOBRUNNER_PORT")
	if port == "" {
		port = "8080"
	}
	basedir := os.Getenv("JOBRUNNER_BASE")
	if basedir == "" {
		basedir, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	hostname := os.Getenv("JOBRUNNER_HOSTNAME")
	go func() {
		cfg := ConfigValues{
			Host:    fmt.Sprintf("%s:%s", hostname, port),
			BaseDir: basedir,
		}
		for cmd := range config {
			switch cmd.action {
			case get:
				cmd.result <- cfg
			case set:
				cfg = cmd.value
			}
		}
	}()
	return err
}

//Get returns a copy of the ConfigValues for jobrunner.
func Get() ConfigValues {
	reply := make(chan interface{})
	req := configMsg{
		action: get,
		result: reply,
	}
	config <- req
	return (<-reply).(ConfigValues)
}

//Set makes the passed in ConfigValues instance the one that every call to Get()
//will return.
func Set(c ConfigValues) {
	config <- configMsg{
		action: set,
		value:  c,
	}
}
