build:
	phpize
	./configure --with-gomesi=../libgomesi
	make

install: build
	make install

test: build
	$(MAKE) -f Makefile test

test-integration: ## Run integration tests
	./test.sh

.PHONY: build install test test-integration
