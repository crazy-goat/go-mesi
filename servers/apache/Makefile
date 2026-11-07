APXS ?= apxs
LIBGOMESI_PATH ?= /usr/lib/libgomesi.so
APR_CANDIDATES ?= $(strip $(shell $(APXS) -q APR_INCLUDEDIR 2>/dev/null)) \
	/opt/homebrew/opt/apr/include/apr-1 \
	/usr/local/opt/apr/include/apr-1 \
	/usr/include/apr-1.0
APR_INCLUDEDIR ?= $(firstword $(foreach d,$(APR_CANDIDATES),$(if $(wildcard $(d)/apr_general.h),$(d))))

all: mod_mesi.so

mod_mesi.so: mod_mesi.c mesi_config.h filter_lifecycle.h cache_config.h parse_json_config.h mesi_pure.h
	$(APXS) -c -I../../libgomesi mod_mesi.c

install: mod_mesi.so
	$(APXS) -i -n mesi mod_mesi.so

clean:
	rm -f mod_mesi.so mod_mesi.la *.lo *.slo test_directives test_pure

test: test-unit test-integration test-build-sh test-apr-includes

test-unit: test-no-mirrors test_directives.c test_pure.c mesi_config.h filter_lifecycle.h cache_config.h mesi_pure.h parse_json_config.h
	$(CC) -o test_directives test_directives.c -I$(APR_INCLUDEDIR) -lapr-1 -laprutil-1
	./test_directives
	$(CC) -o test_pure test_pure.c -I$(APR_INCLUDEDIR) -lapr-1 -laprutil-1
	./test_pure

# Guard: the directive validators/setters/merge and lifecycle helpers live in
# shared headers and are used by mod_mesi.c and the unit tests (#558). Fail if
# a copy is reintroduced in a test, which would let the two drift apart again.
test-no-mirrors:
	./check_no_mirrors.sh

test-integration:
	./test.sh

# Stand-alone bash unit tests for ./build.sh (#102). Runs without Docker,
# Apache, or root. Each scenario stubs apxs / sudo so the install path is
# deterministic regardless of the host environment.
test-build-sh:
	./test_build_sh.sh

test-apr-includes:
	./test_makefile.sh

.PHONY: all install clean test test-unit test-no-mirrors test-integration test-build-sh test-apr-includes
