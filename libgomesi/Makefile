UNAME_S := $(shell uname -s)

# #443: Go's -buildmode=c-shared links with clang's -dynamiclib but passes no
# -install_name, so the c-shared library gets a bare "libgomesi.so" install id.
# dyld only consults LC_RPATH for @rpath/... references, so the PHP extension's
# LC_RPATH (emitted by php-ext/config.m4) was ignored and dlopen failed unless
# DYLD_LIBRARY_PATH was set. Give the dylib an @rpath id on macOS only; Linux and
# every other platform keep the exact previous link flags (and output bytes).
ifeq ($(UNAME_S),Darwin)
C_SHARED_LDFLAGS := -s -w -extldflags '-Wl,-install_name,@rpath/libgomesi.so'
# test-libgomesi records @rpath/libgomesi.so once the library has that id, so
# give it an rpath to the directory it is run from (make test uses ".").
TEST_LDFLAGS := -Wl,-rpath,@loader_path
else
C_SHARED_LDFLAGS := -s -w
TEST_LDFLAGS :=
endif

build:
	go build -trimpath -ldflags="$(C_SHARED_LDFLAGS)" -buildmode=c-shared -o libgomesi.so .
	go build -trimpath -ldflags="-s -w" -buildmode=c-archive -o libgomesi.a .
build-test: build
	gcc -o test-libgomesi test-libgomesi.c -L. -lgomesi -lpthread $(TEST_LDFLAGS)
test: build-test
	LD_LIBRARY_PATH=. ./test-libgomesi
check-darwin: build
	../scripts/check-darwin-rpath.sh libgomesi.so

install:
	cp libgomesi.h /usr/include/
	cp libgomesi.so libgomesi.a /usr/lib/
	ldconfig

.PHONY: build build-test test check-darwin install
