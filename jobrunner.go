package main

import (
	"fmt"
	"log"

	"github.com/johnworth/jobrunner/api"
	"github.com/johnworth/jobrunner/config"
)

func main() {
	fmt.Println("Starting jobrunner.")
	err := config.Configure()
	if err != nil {
		log.Fatal(err)
	}
	h, err := api.NewAPIHandlers()
	if err != nil {
		log.Fatal(err)
	}
	server := h.NewServer()
	log.Fatal(server.ListenAndServe())
}
