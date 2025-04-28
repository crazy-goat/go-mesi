build:
	 apxs -c -I ../../libgomesi/ mod_mesi.c -lgomesi
 clean:
	rm -rf .libs
	rm -f mod_mesi.la mod_mesi.lo mod_mesi.slo
run: build
	MY_DOC_ROOT=$$(realpath "$$(pwd)/../../examples") envsubst < apache.conf.dist > minimal.conf
	apache2 -f $$(pwd)/minimal.conf -DFOREGROUND