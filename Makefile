.PHONY: build test vet fmt run docker e2e

build:
	go build -o bin/crag ./cmd/crag

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

run: build
	./bin/crag serve

docker:
	docker build -t corrective-rag .

e2e:
	./e2e/run.sh
