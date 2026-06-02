build-libgomesi:
	$(MAKE) -C libgomesi build
build-cli:
	$(MAKE) -C cli build
build-test:
	$(MAKE) -C tests build

build-php-ext: build-libgomesi
	cd php-ext && phpize && ./configure --with-gomesi=../libgomesi && make

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