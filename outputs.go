package main

import "io"

// OutputReader will buffer and allow Read()s from data sent via a
// OutputListener. Use the methods associated with the type rather
// than sending messages directly. Note that an EOF will not close
// the channel, Quit() must be called.
type OutputReader chan outputReaderMsg

type outputReaderAction int

const (
	outputReaderSendBytes outputReaderAction = iota
	outputReaderSetEOF
	outputReaderGetEOF
	outputReaderRead
	outputReaderQuit
)

// outputReaderMsg is an incoming message for the OutputReader
type outputReaderMsg struct {
	action  outputReaderAction
	payload []byte
	result  chan interface{}
}

// outputReaderReadMsg is the response to a read request.
type outputReaderReadMsg struct {
	output []byte
	err    error
	read   int
}

// NewOutputReader will create a new OutputReader with the given
// OutputListener.
func NewOutputReader() OutputReader {
	r := make(OutputReader)
	go r.run()
	return r
}

func (r OutputReader) run() {
	var accum []byte
	eof := false

	for cmd := range r {
		switch cmd.action {
		case outputReaderSendBytes:
			inBytes := cmd.payload
			for _, b := range inBytes {
				accum = append(accum, b)
			}
			cmd.result <- 1
		case outputReaderSetEOF:
			eof = true
			cmd.result <- 1
		case outputReaderGetEOF:
			cmd.result <- eof
		case outputReaderQuit:
			break
		case outputReaderRead:
			p := cmd.payload
			if eof && len(accum) == 0 {
				cmd.result <- outputReaderReadMsg{
					err:  io.EOF,
					read: 0,
				}
				continue
			}
			if len(accum) == 0 {
				cmd.result <- outputReaderReadMsg{
					read: 0,
				}
				continue
			}
			var bytesRead int
			if len(accum) <= len(p) {
				bytesRead = copy(p, accum)
				accum = make([]byte, 0)
				if eof {
					cmd.result <- outputReaderReadMsg{
						output: p,
						read:   bytesRead,
						err:    io.EOF,
					}
					continue
				}
				cmd.result <- outputReaderReadMsg{
					output: p,
					read:   bytesRead,
				}
				continue
			}
			bytesRead = copy(p, accum)
			accum = accum[bytesRead:]
			if eof && (len(accum) <= 0) {
				cmd.result <- outputReaderReadMsg{
					output: p,
					read:   bytesRead,
					err:    io.EOF,
				}
				continue
			}
			cmd.result <- outputReaderReadMsg{
				output: p,
				read:   bytesRead,
			}
			continue
		}
	}
}

// Reader will do a consuming read from the OutputReader's buffer. This method
// allows an OutputReader to be substituted for an io.Reader.
func (r OutputReader) Read(p []byte) (n int, err error) {
	reply := make(chan interface{})
	sendMsg := outputReaderMsg{
		action:  outputReaderRead,
		payload: p,
		result:  reply,
	}
	r <- sendMsg
	resp := (<-reply).(outputReaderReadMsg)
	p = resp.output
	return resp.read, resp.err
}

func (r OutputReader) Write(p []byte) (n int, err error) {
	reply := make(chan interface{})
	sendMsg := outputReaderMsg{
		action:  outputReaderSendBytes,
		payload: p,
		result:  reply,
	}
	r <- sendMsg
	<-reply
	return len(p), nil
}

// SetEOF sends an EOF to the goroutine driving the Reader.
func (r OutputReader) SetEOF() {
	reply := make(chan interface{})
	r <- outputReaderMsg{action: outputReaderSetEOF, result: reply}
	<-reply
}

// EOF returns whether or not EOF has been sent.
func (r OutputReader) EOF() bool {
	reply := make(chan interface{})
	r <- outputReaderMsg{action: outputReaderGetEOF, result: reply}
	resp := (<-reply).(bool)
	return resp
}

// Quit will tell the goroutine that pushes data into the buffer to quit.
func (r OutputReader) Quit() {
	r <- outputReaderMsg{action: outputReaderQuit}
	close(r)
}

// outputRegistry contains a list of channels that accept []byte's. Each job
// gets its own OutputRegistry. The OutputRegistry is referred to inside
// the job's Job instance, which in turn is referred to within the the
// Registry.
type outputRegistry struct {
	commands chan outputRegistryCmd
}

type outputRegistryAction int

const (
	registrySet outputRegistryAction = iota
	registryWrite
	registryRemove
	registryFind
	registryQuit
)

type outputRegistryCmd struct {
	action outputRegistryAction
	key    OutputReader
	value  interface{}
	result chan interface{}
}

type outputRegistryFindResult struct {
	found bool
	value OutputReader
}

// NewOutputRegistry returns a pointer to a new instance of OutputRegistry.
func NewOutputRegistry() *outputRegistry {
	l := &outputRegistry{
		commands: make(chan outputRegistryCmd),
	}
	go l.run()
	return l
}

func (o *outputRegistry) run() {
	registry := make(map[OutputReader]OutputReader)
	for command := range o.commands {
		switch command.action {
		case registrySet:
			registry[command.key] = command.value.(OutputReader)
		case registryRemove:
			delete(registry, command.key)
			command.result <- 1
		case registryFind:
			val, ok := registry[command.key]
			command.result <- outputRegistryFindResult{found: ok, value: val}
		case registryQuit:
			for l := range registry {
				l.Quit()
			}
			break
		case registryWrite:
			buf := command.value.([]byte)
			for r := range registry {
				r.Write(buf)
			}
			command.result <- 1
		}
	}
}

// Write sends the []byte array passed out on the Input channel. This
// should allow a BashCommand instance to replace an io.Writer.
func (o *outputRegistry) Write(p []byte) (n int, err error) {
	reply := make(chan interface{})
	o.commands <- outputRegistryCmd{action: registryWrite, value: p, result: reply}
	<-reply
	return len(p), nil
}

// AddListener creates a OutputListener, adds it to the OutputRegistry,
// and returns a pointer to it.
func (o *outputRegistry) AddListener() OutputReader {
	adder := NewOutputReader()
	o.commands <- outputRegistryCmd{key: adder, value: adder, action: registrySet}
	return adder
}

// RemoveListener removes the passed in *OutputListener from the
// OutputRegistry.
func (o *outputRegistry) RemoveListener(l OutputReader) {
	reply := make(chan interface{})
	cmd := outputRegistryCmd{key: l, action: registryRemove, result: reply}
	o.commands <- cmd
	<-reply
}

// HasKey returns true if the *OutputListener is in the registry.
func (o *outputRegistry) HasKey(l OutputReader) bool {
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
