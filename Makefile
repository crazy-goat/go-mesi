build-cli:
	cd cli && go build mesi-cli.go
build-libgomesi:
	cd libgomesi && make
build-test:
	cd tests && go build test-server.go && go build e2e.go

build-php-ext: build-libgomesi
	cd php-ext && phpize && ./configure --with-gomesi=../libgomesi && make

test-php-ext: build-php-ext
	cd php-ext

test-e2e: build-test
	cd tests && ./run-test.sh

all: build-cli build-libgomesi build-test