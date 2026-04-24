APXS ?= apxs
LIBGOMESI_PATH ?= /usr/lib/libgomesi.so

all: mod_mesi.so

mod_mesi.so: mod_mesi.c
	$(APXS) -c -I../../libgomesi mod_mesi.c

install: mod_mesi.so
	$(APXS) -i -n mesi mod_mesi.so

clean:
	rm -f mod_mesi.so mod_mesi.la *.lo *.slo test_directives

test: test-unit test-integration

test-unit: test_directives.c
	$(CC) -o test_directives test_directives.c -I/usr/include/apr-1.0 -lapr-1 -laprutil-1
	./test_directives

test-integration:
	./test.sh

.PHONY: all install clean test test-unit test-integration
