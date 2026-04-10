.PHONY: build test docker-smoke

build:
	go build -o ./bin/codo ./cmd/codo

test:
	go test ./...

docker-smoke:
	./scripts/docker-smoke.sh
