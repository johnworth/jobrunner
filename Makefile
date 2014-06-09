GOPATH=$(CURDIR)/_vendor:$GOPATH

all:
	go build .

test:
	go test .

clean:
	rm ./jobrunner
