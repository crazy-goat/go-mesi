#ifdef HAVE_CONFIG_H
#include "config.h"
#endif

#include <php.h>
#include "../libgomesi/libgomesi.h"

ZEND_BEGIN_ARG_INFO_EX(arginfo_parse, 0, 0, 3)
    ZEND_ARG_INFO(0, input)
    ZEND_ARG_INFO(0, max_depth)
    ZEND_ARG_INFO(0, default_url)
ZEND_END_ARG_INFO()

PHP_FUNCTION(parse) {
    char *input, *default_url;
    size_t input_len, default_url_len;
    zend_long max_depth;

    ZEND_PARSE_PARAMETERS_START(3, 3)
        Z_PARAM_STRING(input, input_len)
        Z_PARAM_LONG(max_depth)
        Z_PARAM_STRING(default_url, default_url_len)
    ZEND_PARSE_PARAMETERS_END();

    char* result = Parse(input, max_depth, default_url);
    RETVAL_STRING(result);
    FreeString(result);
}

PHP_MINIT_FUNCTION(mesi) {
    return SUCCESS;
}

PHP_MSHUTDOWN_FUNCTION(mesi) {
    return SUCCESS;
}

zend_function_entry mesi_functions[] = {
    ZEND_NS_FE("mesi", parse, arginfo_parse)
    PHP_FE_END
};

zend_module_entry mesi_module_entry = {
    STANDARD_MODULE_HEADER,
    "mesi",
    mesi_functions,
    PHP_MINIT(mesi),
    PHP_MSHUTDOWN(mesi),
    NULL,
    NULL,
    NULL,
    "0.1",
    STANDARD_MODULE_PROPERTIES
};

ZEND_GET_MODULE(mesi)
