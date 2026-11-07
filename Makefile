lint:
	bin/lint.sh

build-libgomesi:
	$(MAKE) -C libgomesi build
build-cli:
	$(MAKE) -C cli build
build-test:
	$(MAKE) -C tests build

build-php-ext: build-libgomesi
	cd php-ext && phpize && ./configure --with-gomesi=../libgomesi && make

# #443: macOS only — verifies the @rpath install id on libgomesi.so and, when
# the extension has been built, its @rpath dependency. Prints SKIP elsewhere.
check-darwin: build-libgomesi
	scripts/check-darwin-rpath.sh libgomesi/libgomesi.so $(if $(wildcard php-ext/modules/mesi.so),php-ext/modules/mesi.so)

test-php-ext: build-php-ext
	cd php-ext && make -f GNUmakefile test

test-php-ext-integration:
	cd php-ext && ./test.sh

test-cli-unit:
	$(MAKE) -C cli test

test-cli-e2e:
	$(MAKE) -C cli test-e2e

test-e2e: build-test
	cd tests && ./run-test.sh

all: build-cli build-libgomesi build-test