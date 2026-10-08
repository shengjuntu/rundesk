.PHONY: build test check clean

build:
	CGO_ENABLED=0 go build -buildvcs=false -trimpath -o bin/rundesk ./cmd/rundesk
	CGO_ENABLED=0 go build -buildvcs=false -trimpath -o bin/kun ./cmd/kun

test:
	go test ./...

check:
	go test -race ./...
	go vet ./...

clean:
	rm -f bin/rundesk bin/kun bin/rundesk.exe bin/kun.exe bin/rundesk-windows-amd64.exe
