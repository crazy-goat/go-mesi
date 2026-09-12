APXS ?= apxs
LIBGOMESI_PATH ?= /usr/lib/libgomesi.so
# Query apxs so Homebrew/macOS and Linux both work (#404). Override with
# APR_INCLUDEDIR=...; Debian/Ubuntu path is used when apxs is missing.
APR_INCLUDEDIR ?= $(shell $(APXS) -q APR_INCLUDEDIR 2>/dev/null || echo /usr/include/apr-1.0)

all: mod_mesi.so

mod_mesi.so: mod_mesi.c
	$(APXS) -c -I../../libgomesi mod_mesi.c

install: mod_mesi.so
	$(APXS) -i -n mesi mod_mesi.so

clean:
	rm -f mod_mesi.so mod_mesi.la *.lo *.slo test_directives

test: test-unit test-integration test-build-sh

test-unit: test_directives.c
	$(CC) -o test_directives test_directives.c -I$(APR_INCLUDEDIR) -lapr-1 -laprutil-1
	./test_directives

test-integration:
	./test.sh

# Stand-alone bash unit tests for ./build.sh (#102). Runs without Docker,
# Apache, or root. Each scenario stubs apxs / sudo so the install path is
# deterministic regardless of the host environment.
test-build-sh:
	./test_build_sh.sh

.PHONY: all install clean test test-unit test-integration test-build-sh
