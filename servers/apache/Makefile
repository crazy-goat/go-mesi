APXS ?= apxs
LIBGOMESI_PATH ?= /usr/lib/libgomesi.so
APR_CANDIDATES ?= $(strip $(shell $(APXS) -q APR_INCLUDEDIR 2>/dev/null)) \
	/opt/homebrew/opt/apr/include/apr-1 \
	/usr/local/opt/apr/include/apr-1 \
	/usr/include/apr-1.0
APR_INCLUDEDIR ?= $(firstword $(foreach d,$(APR_CANDIDATES),$(if $(wildcard $(d)/apr_general.h),$(d))))

all: mod_mesi.so

mod_mesi.so: mod_mesi.c
	$(APXS) -c -I../../libgomesi mod_mesi.c

install: mod_mesi.so
	$(APXS) -i -n mesi mod_mesi.so

clean:
	rm -f mod_mesi.so mod_mesi.la *.lo *.slo test_directives

test: test-unit test-integration test-build-sh test-apr-includes

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

test-apr-includes:
	./test_makefile.sh

.PHONY: all install clean test test-unit test-integration test-build-sh test-apr-includes
