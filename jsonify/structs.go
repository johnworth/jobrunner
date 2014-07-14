package jsonify

// JSONCmd represents a single command for a job as sent by a client.
type JSONCmd struct {
	Kind        string
	CommandLine string
	Environment map[string]string
	WorkingDir  string
	Registry    string
	Repository  string
	Tag         string
}

// StartMsg represents a job start request
type StartMsg struct {
	Commands   []JSONCmd
	WorkingDir string
}

// IDMsg represents a ID response
type IDMsg struct {
	JobID      string
	CommandIDs []string
}
