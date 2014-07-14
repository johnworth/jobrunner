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
	Host         string //The hostname:port that jobrunner should use.
	BaseDir      string //The base working directory for the jobs.
	DockerString string //The connection string for the docker client
	DockerUser   string //The docker user for the connection.
	DockerPass   string //The docker password for the connection.
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
	docker := os.Getenv("JOBRUNNER_DOCKER")
	if docker == "" {
		return fmt.Errorf("JOBRUNNER_DOCKER is not set")
	}
	dockerUser := os.Getenv("JOBRUNNER_DOCKER_USER")
	if dockerUser == "" {
		return fmt.Errorf("JOBRUNNER_DOCKER_USER is not set")
	}
	dockerPass := os.Getenv("JOBRUNNER_DOCKER_PASS")
	if dockerPass == "" {
		return fmt.Errorf("JOBRUNNER_DOCKER_PASS is not set")
	}
	go func() {
		cfg := ConfigValues{
			Host:         fmt.Sprintf("%s:%s", hostname, port),
			BaseDir:      basedir,
			DockerString: docker,
			DockerUser:   dockerUser,
			DockerPass:   dockerPass,
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
