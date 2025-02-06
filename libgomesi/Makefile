build:
	go build -trimpath -ldflags="-s -w" -buildmode=c-shared -o libgomesi.so libgomesi.go
	cd test-lib && gcc -o test-libgomesi test-libgomesi.c -L../ -lgomesi
test: build
	LD_LIBRARY_PATH=. ./test-lib/libgomesi-test ../examples/simple.html

install:
	cp libgomesi.so /usr/lib/
	ldconfig