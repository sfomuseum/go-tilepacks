tools:
	go build -mod vendor -o bin/build cmd/build/main.go
	go build -mod vendor -o bin/merge cmd/merge/main.go
	go build -mod vendor -o bin/serve cmd/serve/main.go
	go build -mod vendor -o bin/mbtiles-assign-metadata cmd/mbtiles-assign-metadata/main.go
