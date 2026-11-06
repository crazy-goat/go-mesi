--TEST--
parse_with_config() debug=true emits core parser diagnostics on stderr
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
// #216: debug=true enables mesi.EsiParserConfig.Debug via libgomesi's
// ParseJson. The core's DefaultLogger writes DEBUG lines to STDERR and
// run-tests merges fd2 into the compared output by default (2>&1), so a
// missing "DEBUG parse_start" line means the key never reached the core.
// The leading %A in --EXPECTF-- absorbs the RFC3339 timestamp and any
// other diagnostics the core emits for this parse.
echo \mesi\parse_with_config('<p>debug-plain</p>', 5, 'http://127.0.0.1/', ['debug' => true]);
?>
--EXPECTF--
%ADEBUG parse_start%A<p>debug-plain</p>
