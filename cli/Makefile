build:
	go mod tidy
	go mod download
	go build mesi-cli.go

test: build
	go test -v -count=1 ./...

test-e2e:
	./test.sh