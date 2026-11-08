.PHONY: build package test check clean

PACKAGE_DIR ?= ../rundesk-portable
KUN_BINARY ?=

build:
	CGO_ENABLED=0 go build -buildvcs=false -trimpath -o bin/rundesk ./cmd/rundesk

package: build
	python3 scripts/package-release.py --binary bin/rundesk --output-dir "$(PACKAGE_DIR)" $(if $(KUN_BINARY),--kun "$(KUN_BINARY)",)

test:
	go test ./...
	cd third_party/kun-sdk && go test ./...

check:
	go test -race ./...
	go vet ./...
	cd third_party/kun-sdk && go test -race ./...
	cd third_party/kun-sdk && go vet ./...

clean:
	rm -f bin/rundesk bin/rundesk.exe bin/rundesk-windows-amd64.exe
