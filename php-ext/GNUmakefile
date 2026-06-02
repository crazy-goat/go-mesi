build:
	phpize
	./configure --with-gomesi=../libgomesi
	$(MAKE) -f Makefile

install: build
	$(MAKE) -f Makefile install

test: build
	$(MAKE) -f Makefile test

test-integration: ## Run integration tests
	./test.sh

.PHONY: build install test test-integration
