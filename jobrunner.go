package main

import (
	"fmt"
	"log"
)

func main() {
	fmt.Println("Starting jobrunner.")
	server := NewAPIHandlers().NewServer()
	log.Fatal(server.ListenAndServe())
}
