package main

import (
	"fmt"
	"log"
	"os"
)

// Config contains the configuration values for jobrunner.
type Config struct {
	Host string
}

// Configure gathers the configuration values for jobrunner and returns a
// pointer to a filled out Config instance.
func Configure() *Config {
	port := os.Getenv("JOBRUNNER_PORT")
	if port == "" {
		port = "8080"
	}
	hostname := os.Getenv("JOBRUNNER_HOSTNAME")
	c := &Config{
		Host: fmt.Sprintf("%s:%s", hostname, port),
	}
	return c
}

func main() {
	fmt.Println("Starting jobrunner.")
	conf := Configure()
	server := NewAPIHandlers().NewServer(conf)
	log.Fatal(server.ListenAndServe())
}
