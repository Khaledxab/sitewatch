.PHONY: test build docker run
test:
	go vet ./... && go test -race ./...
build:
	CGO_ENABLED=0 go build -trimpath -o bin/sitewatch .
docker:
	docker build -t sitewatch:dev .
run: build
	./bin/sitewatch serve -targets targets.example.txt
