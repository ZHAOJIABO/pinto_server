.PHONY: proto build run clean fmt

proto:
	buf generate

proto-lint:
	buf lint

build:
	go build -o bin/server ./cmd/

run:
	go run ./cmd/ -config conf/server.yaml

clean:
	rm -rf bin/

fmt:
	gofmt -w .
	goimports -w .

tidy:
	go mod tidy

test:
	go test ./...

docker-build:
	docker build -t bobobeads-server .

docker-up:
	docker-compose up -d

docker-down:
	docker-compose down
