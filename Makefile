GOMOD=$(shell test -f "go.work" && echo "readonly" || echo "vendor")

tools:
	go build -mod $(GOMOD) -ldflags="-s -w" -o bin/build cmd/build/main.go
	go build -mod $(GOMOD) -ldflags="-s -w" -o bin/merge cmd/merge/main.go
	go build -mod $(GOMOD) -ldflags="-s -w" -o bin/serve cmd/serve/main.go
