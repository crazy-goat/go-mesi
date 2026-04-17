APXS ?= apxs
LIBGOMESI_PATH ?= /usr/lib/libgomesi.so

all: mod_mesi.so

mod_mesi.so: mod_mesi.c
	$(APXS) -c -I../../libgomesi mod_mesi.c

install: mod_mesi.so
	$(APXS) -i -n mesi mod_mesi.so

clean:
	rm -f mod_mesi.so mod_mesi.la *.lo *.slo

test:
	./test.sh

.PHONY: all install clean test
