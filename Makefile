build-cli:
	cd cli && go build mesi-cli.go
build-libgomesi:
	cd libgomesi && make
build-php-ext: build-libgomesi
	cd php-ext && phpize && ./configure --with-gomesi=../libgomesi && make

test-php-ext: build-php-ext
	cd php-ext
