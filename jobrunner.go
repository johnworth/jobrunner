package main

import (
	"fmt"
	"log"

	"github.com/johnworth/jobrunner/api"
	"github.com/johnworth/jobrunner/config"
)

func main() {
	fmt.Println("Starting jobrunner.")
	conf := config.Configure()
	server := api.NewAPIHandlers().NewServer(conf)
	log.Fatal(server.ListenAndServe())
}
