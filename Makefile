####GOPATH=$(CURDIR)/_vendor:$GOPATH
GOPATH=$(CURDIR)/_vendor:$(realpath ../../../../):$GOPATH


all:
	go build .

test:
	go test ./api
	go test ./executor
	go test ./filesystem
	go test ./jobs

clean:
	rm ./jobrunner
