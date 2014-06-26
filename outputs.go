package main

import (
	"fmt"
	"io"
	"sync"
)

// OutputListener maintains a Listener and Quit channel for data read from
// a job's stdout/stderr stream.
type OutputListener struct {
	Listener chan []byte
	Quit     chan int
}

// NewOutputListener returns a new instance of OutputListener.
func NewOutputListener() *OutputListener {
	return &OutputListener{
		Listener: make(chan []byte),
		Quit:     make(chan int),
	}
}

// OutputReader will buffer and allow Read()s from data sent via a
// OutputListener
type OutputReader struct {
	accum       []byte
	listener    *OutputListener
	m           *sync.Mutex
	quitChannel chan int
	eof         bool
}

// NewOutputReader will create a new OutputReader with the given
// OutputListener.
func NewOutputReader(l *OutputListener) *OutputReader {
	r := &OutputReader{
		accum:       make([]byte, 0),
		listener:    l,
		m:           &sync.Mutex{},
		quitChannel: make(chan int),
		eof:         false,
	}
	go r.run()
	return r
}

func (r *OutputReader) run() {
	for {
		select {
		case inBytes := <-r.listener.Listener:
			r.m.Lock()
			//fmt.Println(string(inBytes[:]))
			for _, b := range inBytes {
				r.accum = append(r.accum, b)
			}
			r.m.Unlock()
		case <-r.listener.Quit: //Quit if the listener tells us to.
			r.m.Lock()
			fmt.Println("setting r.eof from a listener.Quit")
			//r.eof = true
			r.m.Unlock()
			break
		case <-r.quitChannel: //Quit if a caller tells us to.
			r.m.Lock()
			fmt.Println("setting r.eof from a r.quitChannle")
			//r.eof = true
			r.m.Unlock()
			break
		}
	}
}

// Reader will do a consuming read from the OutputReader's buffer. This method
// allows an OutputReader to be substituted for an io.Reader.
func (r *OutputReader) Read(p []byte) (n int, err error) {
	r.m.Lock()
	defer r.m.Unlock()
	if r.eof && len(r.accum) == 0 {
		return 0, io.EOF
	}
	if len(r.accum) == 0 {
		return 0, nil
	}
	var bytesRead int
	if len(r.accum) <= len(p) {
		bytesRead = copy(p, r.accum)
		r.accum = make([]byte, 0)
		if r.eof {
			return bytesRead, io.EOF
		}
		return bytesRead, nil
	}
	bytesRead = copy(p, r.accum)
	r.accum = r.accum[bytesRead:]
	if r.eof && (len(r.accum) <= 0) {
		return bytesRead, io.EOF
	}
	return bytesRead, nil

}

// Quit will tell the goroutine that pushes data into the buffer to quit.
func (r *OutputReader) Quit() {
	r.m.Lock()
	r.eof = true
	r.m.Unlock()
	r.quitChannel <- 1
}

// outputRegistry contains a list of channels that accept []byte's. Each job
// gets its own OutputRegistry. The OutputRegistry is referred to inside
// the job's Job instance, which in turn is referred to within the the
// Registry.
type outputRegistry struct {
	commands chan outputRegistryCmd
	Input    chan []byte
}

type outputRegistryAction int

const (
	registrySet outputRegistryAction = iota
	registryRemove
	registryFind
	registryQuit
)

type outputRegistryCmd struct {
	action outputRegistryAction
	key    *OutputListener
	value  chan []byte
	result chan interface{}
}

type outputRegistryFindResult struct {
	found bool
	value chan []byte
}

// NewOutputRegistry returns a pointer to a new instance of OutputRegistry.
func NewOutputRegistry() *outputRegistry {
	l := &outputRegistry{
		commands: make(chan outputRegistryCmd),
		Input:    make(chan []byte),
	}
	go l.run()
	return l
}

func (o *outputRegistry) run() {
	registry := make(map[*OutputListener]chan []byte)
	for {
		select {
		case command := <-o.commands:
			switch command.action {
			case registrySet:
				registry[command.key] = command.value
			case registryRemove:
				delete(registry, command.key)
				command.result <- 1
			case registryFind:
				val, ok := registry[command.key]
				command.result <- outputRegistryFindResult{found: ok, value: val}
			case registryQuit:
				for l := range registry {
					l.Quit <- 1
				}
				close(o.commands)
			}
		case buf := <-o.Input: //Demuxing the output to the listeners
			for _, ch := range registry {
				ch <- buf
			}
		}
	}
}

// Write sends the []byte array passed out on the Input channel. This
// should allow a BashCommand instance to replace an io.Writer.
func (o *outputRegistry) Write(p []byte) (n int, err error) {
	o.Input <- p
	return len(p), nil
}

// AddListener creates a OutputListener, adds it to the OutputRegistry,
// and returns a pointer to it.
func (o *outputRegistry) AddListener() *OutputListener {
	adder := NewOutputListener()
	o.commands <- outputRegistryCmd{key: adder, value: adder.Listener, action: registrySet}
	return adder
}

// RemoveListener removes the passed in *OutputListener from the
// OutputRegistry.
func (o *outputRegistry) RemoveListener(l *OutputListener) {
	reply := make(chan interface{})
	cmd := outputRegistryCmd{key: l, action: registryRemove, result: reply}
	o.commands <- cmd
	<-reply
}

// HasKey returns true if the *OutputListener is in the registry.
func (o *outputRegistry) HasKey(l *OutputListener) bool {
	reply := make(chan interface{})
	cmd := outputRegistryCmd{key: l, action: registryFind, result: reply}
	o.commands <- cmd
	result := (<-reply).(outputRegistryFindResult)
	return result.found
}

// Quit tells the OutputRegistry's goroutine to exit.
func (o *outputRegistry) Quit() {
	o.commands <- outputRegistryCmd{action: registryQuit}
}
