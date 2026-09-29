.PHONY: build test check clean

build:
	CGO_ENABLED=0 go build -buildvcs=false -trimpath -o bin/rundesk ./cmd/rundesk

test:
	go test ./...

check:
	go test -race ./...
	go vet ./...

clean:
	rm -f bin/rundesk bin/rundesk-windows-amd64.exe
