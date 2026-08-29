--TEST--
parse_with_config() cache_key_template validation
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
$warnings = [];
set_error_handler(function ($errno, $errstr) use (&$warnings) {
    if ($errno === E_WARNING) { $warnings[] = $errstr; return true; }
    return false;
});
function run(array $cfg) {
    global $warnings;
    $before = count($warnings);
    $r = @\mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', $cfg);
    return [$r, array_slice($warnings, $before)];
}

// 1. wrong type: int
list($r,$w)=run(['cache_key_template'=>42]);
echo "int_result=".($r===false?'false':'string')."\n";
echo "int_warnings=".count($w)."\n";

// 2. wrong type: array
list($r,$w)=run(['cache_key_template'=>['mesi:${url}']]);
echo "array_result=".($r===false?'false':'string')."\n";
echo "array_warnings=".count($w)."\n";

// 3. space (mesi_is_safe_string rejects space/tab)
list($r,$w)=run(['cache_backend'=>'memory','cache_key_template'=>'mesi:${url} with space']);
echo "space_result=".($r===false?'false':'string')."\n";
echo "space_warnings=".count($w)."\n";

// 4. control char (0x01)
list($r,$w)=run(['cache_backend'=>'memory','cache_key_template'=>"bad\x01tmpl"]);
echo "ctrl_result=".($r===false?'false':'string')."\n";
echo "ctrl_warnings=".count($w)."\n";

// 5. template set, no cache_backend => silently ignored, no warning, string result
list($r,$w)=run(['cache_key_template'=>'mesi:${url}:${header:Accept-Language}']);
echo "no_backend_result=".($r===false?'false':'string')."\n";
echo "no_backend_warnings=".count($w)."\n";
echo "no_backend_body=".($r===false?'':$r)."\n";

// 6. empty template => unset, no warning
list($r,$w)=run(['cache_backend'=>'memory','cache_key_template'=>'']);
echo "empty_result=".($r===false?'false':'string')."\n";
echo "empty_warnings=".count($w)."\n";

// 7. request_headers wrong type (string instead of array)
list($r,$w)=run(['cache_backend'=>'memory','cache_key_template'=>'mesi:${url}','request_headers'=>'bad']);
echo "headers_wrongtype_result=".($r===false?'false':'string')."\n";
echo "headers_wrongtype_warnings=".count($w)."\n";

// 8. request_headers bad value (control char)
list($r,$w)=run(['cache_backend'=>'memory','cache_key_template'=>'mesi:${url}','request_headers'=>['Accept-Language'=>"bad\x01"]]);
echo "headers_badval_result=".($r===false?'false':'string')."\n";
echo "headers_badval_warnings=".count($w)."\n";

// 9. request_headers array value with non-string element
list($r,$w)=run(['cache_backend'=>'memory','cache_key_template'=>'mesi:${url}','request_headers'=>['X'=>[42]]]);
echo "headers_array_bad_result=".($r===false?'false':'string')."\n";
echo "headers_array_bad_warnings=".count($w)."\n";

// 10. request_cookies wrong type
list($r,$w)=run(['cache_backend'=>'memory','cache_key_template'=>'mesi:${url}','request_cookies'=>'bad']);
echo "cookies_wrongtype_result=".($r===false?'false':'string')."\n";
echo "cookies_wrongtype_warnings=".count($w)."\n";

// 11. request_cookies bad value (contains ")
list($r,$w)=run(['cache_backend'=>'memory','cache_key_template'=>'mesi:${url}','request_cookies'=>['ab'=>'bad"val']]);
echo "cookies_badval_result=".($r===false?'false':'string')."\n";
echo "cookies_badval_warnings=".count($w)."\n";

// 12. request_cookies bad value (control char)
list($r,$w)=run(['cache_backend'=>'memory','cache_key_template'=>'mesi:${url}','request_cookies'=>['ab'=>"bad\x01"]]);
echo "cookies_ctrl_result=".($r===false?'false':'string')."\n";
echo "cookies_ctrl_warnings=".count($w)."\n";

// 13. request_cookies empty key
list($r,$w)=run(['cache_backend'=>'memory','cache_key_template'=>'mesi:${url}','request_cookies'=>[''=>'v']]);
echo "cookies_emptykey_result=".($r===false?'false':'string')."\n";
echo "cookies_emptykey_warnings=".count($w)."\n";

// 14. valid minimal with template + headers + cookies
list($r,$w)=run(['cache_backend'=>'memory','cache_key_template'=>'mesi:${url}','request_headers'=>['Accept-Language'=>'pl'],'request_cookies'=>['ab'=>'v1']]);
echo "valid_result=".($r===false?'false':'string')."\n";
echo "valid_warnings=".count($w)."\n";

// 15. NUL byte in template => E_WARNING + false
list($r,$w)=run(['cache_backend'=>'memory','cache_key_template'=>"a\0b"]);
echo "nul_result=".($r===false?'false':'string')."\n";
echo "nul_warnings=".count($w)."\n";

// 16. headers+cookies with EMPTY template and backend=memory => silently ignored
list($r,$w)=run(['cache_backend'=>'memory','cache_key_template'=>'','request_headers'=>['Accept'=>'text/html'],'request_cookies'=>['ab'=>'v1']]);
echo "empty_with_ctx_result=".($r===false?'false':'string')."\n";
echo "empty_with_ctx_warnings=".count($w)."\n";

// 17. valid headers array-of-strings with NON-empty template => exercises array branch
list($r,$w)=run(['cache_backend'=>'memory','cache_key_template'=>'mesi:${url}','request_headers'=>['Accept'=>['text/html','application/json']]]);
echo "array_header_result=".($r===false?'false':'string')."\n";
echo "array_header_warnings=".count($w)."\n";
?>
--EXPECT--
int_result=false
int_warnings=1
array_result=false
array_warnings=1
space_result=false
space_warnings=1
ctrl_result=false
ctrl_warnings=1
no_backend_result=string
no_backend_warnings=0
no_backend_body=plain-ok
empty_result=string
empty_warnings=0
headers_wrongtype_result=false
headers_wrongtype_warnings=1
headers_badval_result=false
headers_badval_warnings=1
headers_array_bad_result=false
headers_array_bad_warnings=1
cookies_wrongtype_result=false
cookies_wrongtype_warnings=1
cookies_badval_result=false
cookies_badval_warnings=1
cookies_ctrl_result=false
cookies_ctrl_warnings=1
cookies_emptykey_result=false
cookies_emptykey_warnings=1
valid_result=string
valid_warnings=0
nul_result=false
nul_warnings=1
empty_with_ctx_result=string
empty_with_ctx_warnings=0
array_header_result=string
array_header_warnings=0
