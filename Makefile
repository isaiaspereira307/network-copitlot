.PHONY: build build-linux build-windows dist test vet clean

build:
	CGO_ENABLED=0 go build -trimpath -o mcp-proxy ./cmd/mcp-proxy

test:
	go test -race ./...

vet:
	go vet ./...

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/mcp-proxy ./cmd/mcp-proxy

build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o dist/mcp-proxy.exe ./cmd/mcp-proxy

dist: build-linux build-windows

clean:
	rm -rf dist
