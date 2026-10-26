.PHONY: build test check windows clean

build:
	CGO_ENABLED=0 go build -buildvcs=false -trimpath -o bin/kun ./cmd/kun

test:
	go test ./...
	cd sdk && go test ./...

check:
	go test -race ./...
	go vet ./...
	cd sdk && go test -race ./...
	cd sdk && go vet ./...

windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -buildvcs=false -trimpath -o bin/kun-windows-amd64.exe ./cmd/kun

clean:
	rm -f bin/kun bin/kun.exe bin/kun-windows-amd64.exe
