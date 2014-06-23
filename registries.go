package main

// RegistryCmd represents a command sent to the registry
type registryCommand struct {
	action registryAction
	key    string
	value  interface{}
	result chan<- interface{}
}

type registryAction int

const (
	remove registryAction = iota
	find
	set
	get
	length
	quit
	listkeys
)

// Registry encapsulates a map associating a string with a *Job. This
// will let additional goroutines communicate with the goroutine running the
// job associated with the key. The key will most likely be a UUID. There should
// only be one instance on Registry per jobrunner instance.
type Registry chan registryCommand

// NewRegistry creates a new instance of Registry and returns a pointer to
// it. Does not make sure that only one instance is created.
func NewRegistry() Registry {
	r := make(Registry)
	go r.run()
	return r
}

type registryFindResult struct {
	found  bool
	result *Job
}

// Listen launches a goroutine that can be communicated with via the setter
// and getter channels passed in. Listen is non-blocking.
func (r Registry) run() {
	reg := make(map[string]*Job)
	for command := range r {
		switch command.action {
		case set:
			reg[command.key] = command.value.(*Job)
		case get:
			val, found := reg[command.key]
			command.result <- registryFindResult{found, val}
		case length:
			command.result <- len(reg)
		case remove:
			delete(reg, command.key)
		case listkeys:
			var retval []string
			for k := range reg {
				retval = append(retval, k)
			}
			command.result <- retval
		case quit:
			close(r)
		}
	}
}

// Register adds the *Job to the registry with the key set to the
// value of uuid.
func (r Registry) Register(uuid string, s *Job) {
	r <- registryCommand{action: set, key: uuid, value: s}
}

// Get looks up the job for the given uuid in the registry.
func (r Registry) Get(uuid string) *Job {
	reply := make(chan interface{})
	regCmd := registryCommand{action: get, key: uuid, result: reply}
	r <- regCmd
	result := (<-reply).(registryFindResult)
	return result.result
}

// HasKey returns true if a job associated with uuid is running, false otherwise.
func (r Registry) HasKey(uuid string) bool {
	reply := make(chan interface{})
	regCmd := registryCommand{action: get, key: uuid, result: reply}
	r <- regCmd
	result := (<-reply).(registryFindResult)
	return result.found
}

// List returns the list of jobs from the registry.
func (r Registry) List() []string {
	reply := make(chan interface{})
	regCmd := registryCommand{action: listkeys, result: reply}
	r <- regCmd
	result := (<-reply).([]string)
	return result
}

// Delete deletes a *Job from the registry.
func (r Registry) Delete(uuid string) {
	r <- registryCommand{action: remove, key: uuid}
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

// Listen fires off a goroutine that can be communicated with through the Input,
// Setter, and Remove channels.
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

// AddListener creates a OutputListener, adds it to the OutputRegistry,
// and returns a pointer to the OutputListener. Synchronizes with the
// OutputRegistry goroutine through the OutputListener's Latch channel.
func (o *outputRegistry) AddListener() *OutputListener {
	adder := NewOutputListener()
	o.commands <- outputRegistryCmd{key: adder, value: adder.Listener, action: registrySet}
	return adder
}

// RemoveListener removes the passed in OutputListener from the
// OutputRegistry. Synchronizes with the JobOuputRegistry goroutine through
// the OutputListener's Latch channel. Does not close any channels.
func (o *outputRegistry) RemoveListener(l *OutputListener) {
	reply := make(chan interface{})
	cmd := outputRegistryCmd{key: l, action: registryRemove, result: reply}
	o.commands <- cmd
	<-reply
}

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
