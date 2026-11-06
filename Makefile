BINARY_NAME=wppserver
GO ?= go
LDFLAGS ?= -s -w

.PHONY: build test vet docker compile

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

docker:
	docker build -t $(BINARY_NAME):local .

compile:
	echo "Compiling for every OS and Platform"
	GOOS=windows GOARCH=amd64 $(GO) build -ldflags="$(LDFLAGS)" -o bin/${BINARY_NAME}-windows-amd64.exe ./cmd
	GOOS=darwin GOARCH=amd64 $(GO) build -ldflags="$(LDFLAGS)" -o bin/${BINARY_NAME}-darwin-amd64 ./cmd
	GOOS=darwin GOARCH=arm64 $(GO) build -ldflags="$(LDFLAGS)" -o bin/${BINARY_NAME}-darwin-arm64 ./cmd
	GOOS=linux GOARCH=amd64 $(GO) build -ldflags="$(LDFLAGS)" -o bin/${BINARY_NAME}-linux-amd64 ./cmd
	GOOS=linux GOARCH=arm64 $(GO) build -ldflags="$(LDFLAGS)" -o bin/${BINARY_NAME}-linux-arm64 ./cmd
	GOOS=freebsd GOARCH=amd64 $(GO) build -ldflags="$(LDFLAGS)" -o bin/${BINARY_NAME}-freebsd-amd64 ./cmd
	GOOS=openbsd GOARCH=amd64 $(GO) build -ldflags="$(LDFLAGS)" -o bin/${BINARY_NAME}-openbsd-amd64 ./cmd
