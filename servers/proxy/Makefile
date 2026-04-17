.PHONY: build run clean test docker-build docker-run

BINARY_NAME=mesi-proxy

build:
	go build -o $(BINARY_NAME) .

run: build
	./$(BINARY_NAME) --backend http://127.0.0.1:8081

clean:
	rm -f $(BINARY_NAME)

test:
	go test -v ./...

docker-build:
	docker build -t mesi-proxy:latest .

docker-run:
	docker run --rm -p 8080:8080 -e BACKEND=http://host.docker.internal:8081 mesi-proxy:latest