build:
	go build -trimpath -ldflags="-s -w" -buildmode=c-shared -o libgomesi.so libgomesi.go
build-test: build
	gcc -o test-libgomesi test-libgomesi.c -L../ -lgomesi
test: build-test
	LD_LIBRARY_PATH=. ./test-libgomesi ../examples/simple.html

install:
	cp libgomesi.so /usr/lib/
	ldconfig