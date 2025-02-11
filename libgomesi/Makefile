build:
	go build -trimpath -ldflags="-s -w" -buildmode=c-shared -o libgomesi.so libgomesi.go
	go build -trimpath -ldflags="-s -w" -buildmode=c-archive -o libgomesi.a libgomesi.go
build-test: build
	gcc -o test-libgomesi test-libgomesi.c -L../ -lgomesi
test: build-test
	LD_LIBRARY_PATH=. ./test-libgomesi ../examples/simple.html

install:
	cp libgomesi.h /usr/include/
	cp libgomesi.so libgomesi.a /usr/lib/
	ldconfig