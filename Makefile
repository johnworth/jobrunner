GOPATH=$(CURDIR)/_vendor:$(realpath ../../../../):$GOPATH

all:
	go build .

test:
	go test ./api
	go test ./executor
	go test ./jobs
	go test ./config

clean:
	rm ./jobrunner
