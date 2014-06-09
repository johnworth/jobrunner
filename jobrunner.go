package main

import (
	"fmt"
)

func main() {
	fmt.Println("Starting jobrunner.")
	js := &JobSyncer{
		Command:  make(chan string),
		Start:    make(chan int),
		Output:   make(chan []byte),
		ExitCode: make(chan int),
		Registry: make([]Listener, 10),
	}

	for i := range js.Registry {
		js.Registry[i] = Listener{
			Listen: make(chan []byte),
		}
	}

	exe := jobExecutor()
	go exe(js)
	js.Command <- "echo foo bar baz blippy"
	js.Start <- 1

	for j := range js.Registry {
		output := <-js.Registry[j].Listen
		fmt.Println(string(output[:]))
	}
	fmt.Println(<-js.ExitCode)
	ServeHTTP()
}
