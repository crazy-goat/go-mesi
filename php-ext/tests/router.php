<?php
$path = parse_url($_SERVER['REQUEST_URI'], PHP_URL_PATH);
$backend = getenv('MESI_BACKEND_URL') ?: 'http://test-server/';
$esiIncludeUrl = rtrim($backend, '/') . '/esi';

if ($path === '/') {
    header('Content-Type: text/html');
    echo \mesi\parse(
        '<!DOCTYPE html><html><body>'
        . '<h1>ESI PHP Extension Test</h1>'
        . '<esi:include src="' . $esiIncludeUrl . '" />'
        . '<esi:remove>Failed to include ESI</esi:remove>'
        . '<!--esi <p>Unwrapped content</p> -->'
        . '</body></html>',
        5,
        $backend
    );
    return true;
}

if ($path === '/plain') {
    header('Content-Type: text/plain');
    echo \mesi\parse(
        'plain text with <esi:include src="http://test-server/esi" /> tags',
        5,
        $backend
    );
    return true;
}

if ($path === '/json') {
    header('Content-Type: application/json');
    echo \mesi\parse(
        json_encode([
            'message' => 'ESI test',
            'content' => '<esi:include src="http://test-server/esi" />'
        ]),
        5,
        $backend
    );
    return true;
}

if ($path === '/remove') {
    header('Content-Type: text/html');
    echo \mesi\parse(
        '<p>keep this</p><esi:remove>remove this</esi:remove><p>also keep this</p>',
        5,
        $backend
    );
    return true;
}

// allowed_hosts: the configured backend's hostname is whitelisted, so the
// include resolves (block_private_ips=false lets the loopback/private dial
// through in CI where MESI_BACKEND_URL points at 127.0.0.1).
if ($path === '/allowed-hosts') {
    header('Content-Type: text/html');
    $host = parse_url($backend, PHP_URL_HOST) ?: '';
    echo \mesi\parse_with_config(
        '<p>allowed test</p><esi:include src="' . $esiIncludeUrl . '" />',
        5,
        $backend,
        ['allowed_hosts' => (string)$host, 'block_private_ips' => false]
    );
    return true;
}

// allowed_hosts: a hostname NOT in the whitelist is blocked before any
// dial — the fragment must never appear in the response.
if ($path === '/allowed-hosts-blocked') {
    header('Content-Type: text/html');
    echo \mesi\parse_with_config(
        '<p>blocked test</p><esi:include src="' . $esiIncludeUrl . '" />',
        5,
        $backend,
        ['allowed_hosts' => 'example.com', 'block_private_ips' => false]
    );
    return true;
}

// allowed_hosts subdomain: 'sub.test-server' is a subdomain of the allowed
// host 'test-server'. Intended for the docker-compose fixture where the
// test-server service carries the `sub.test-server` network alias. Not
// exercised in CI, where test.sh runs without docker and the alias does not
// exist.
if ($path === '/allowed-hosts-subdomain') {
    header('Content-Type: text/html');
    echo \mesi\parse_with_config(
        '<p>subdomain test</p><esi:include src="http://sub.test-server/esi" />',
        5,
        $backend,
        ['allowed_hosts' => 'test-server', 'block_private_ips' => false]
    );
    return true;
}

// allow_private_ips_for_allowed_hosts: bypass ON. block_private_ips stays
// TRUE — the whitelisted backend host resolves to a private/reserved IP
// (loopback in CI where MESI_BACKEND_URL=http://localhost:8081/, a docker
// container IP otherwise), so only the per-host bypass can let the dial
// through. Proves the libgomesi shared-client yield end-to-end: without it
// the bypass is a silent no-op and this include is blocked.
if ($path === '/bypass-on') {
    header('Content-Type: text/html');
    $host = parse_url($backend, PHP_URL_HOST) ?: '';
    echo \mesi\parse_with_config(
        '<p>bypass on</p><esi:include src="' . $esiIncludeUrl . '" />',
        5,
        $backend,
        [
            'allowed_hosts' => (string)$host,
            'block_private_ips' => true,
            'allow_private_ips_for_allowed_hosts' => true,
        ]
    );
    return true;
}

// bypass OFF (default): same whitelisted host, private dial must stay
// blocked.
if ($path === '/bypass-off') {
    header('Content-Type: text/html');
    $host = parse_url($backend, PHP_URL_HOST) ?: '';
    echo \mesi\parse_with_config(
        '<p>bypass off</p><esi:include src="' . $esiIncludeUrl . '" />',
        5,
        $backend,
        ['allowed_hosts' => (string)$host, 'block_private_ips' => true]
    );
    return true;
}

// bypass ON but the backend host is NOT in allowed_hosts -> rejected by the
// whitelist before any dial; the bypass only covers whitelisted hosts.
if ($path === '/bypass-unlisted') {
    header('Content-Type: text/html');
    echo \mesi\parse_with_config(
        '<p>bypass unlisted</p><esi:include src="' . $esiIncludeUrl . '" />',
        5,
        $backend,
        [
            'allowed_hosts' => 'example.com',
            'block_private_ips' => true,
            'allow_private_ips_for_allowed_hosts' => true,
        ]
    );
    return true;
}

// timeout (#181): the include is fetched from a slow fragment server on
// 127.0.0.1:18081 (dedicated `php -S` spawned by test.sh — see
// tests/slow_router.php, 4s per response). block_private_ips=false so the
// loopback dial is allowed and only the budget can fail the include.
if ($path === '/timeout') {
    header('Content-Type: text/html');
    echo \mesi\parse_with_config(
        '<p>timeout test</p><esi:include src="http://127.0.0.1:18081/fragment" />',
        5,
        $backend,
        ['timeout' => 2, 'block_private_ips' => false]
    );
    return true;
}

// timeout=30: well above the 4s sleep -> include succeeds (issue AC:
// "timeout: 30 + normal include -> success").
if ($path === '/timeout-ok') {
    header('Content-Type: text/html');
    echo \mesi\parse_with_config(
        '<p>timeout ok</p><esi:include src="http://127.0.0.1:18081/fragment" />',
        5,
        $backend,
        ['timeout' => 30, 'block_private_ips' => false]
    );
    return true;
}

// timeout key absent: documented default 30s applies and the call stays on
// the positional ParseWithConfigCtx path (backward compatible).
if ($path === '/timeout-default') {
    header('Content-Type: text/html');
    echo \mesi\parse_with_config(
        '<p>timeout default</p><esi:include src="http://127.0.0.1:18081/fragment" />',
        5,
        $backend,
        ['block_private_ips' => false]
    );
    return true;
}

// max_response_size (#201): fragment bodies come from a DEDICATED
// size-serving server spawned by test.sh on 127.0.0.1:18082 (php -S is
// single-threaded — the include must never be served by this process, same
// rule as the slow server above). block_private_ips=false so the loopback
// dial is allowed and only the size cap can fail the include.
if ($path === '/max-response-size-over') {
    header('Content-Type: text/html');
    echo \mesi\parse_with_config(
        '<p>over test</p><esi:include src="http://127.0.0.1:18082/bytes/200" />',
        5,
        $backend,
        ['max_response_size' => 100, 'block_private_ips' => false]
    );
    return true;
}

// 200-byte body under a 1000-byte cap -> delivered fully.
if ($path === '/max-response-size-under') {
    header('Content-Type: text/html');
    echo \mesi\parse_with_config(
        '<p>under test</p><esi:include src="http://127.0.0.1:18082/bytes/200" />',
        5,
        $backend,
        ['max_response_size' => 1000, 'block_private_ips' => false]
    );
    return true;
}

// key absent: documented default 0 = UNLIMITED — a 10 MB + 1-byte body
// still delivers (pins absent = unlimited against the issue's proposed
// "absent -> 10 MB" default, which never exists on this path — #169).
if ($path === '/max-response-size-absent') {
    header('Content-Type: text/html');
    echo \mesi\parse_with_config(
        '<p>absent test</p><esi:include src="http://127.0.0.1:18082/bytes/10485761" />',
        5,
        $backend,
        ['block_private_ips' => false]
    );
    return true;
}

// explicit 0: the documented "unlimited" value (mesi/fetch.go only limits
// when MaxResponseSize > 0) — same large body must deliver.
if ($path === '/max-response-size-zero') {
    header('Content-Type: text/html');
    echo \mesi\parse_with_config(
        '<p>zero test</p><esi:include src="http://127.0.0.1:18082/bytes/10485761" />',
        5,
        $backend,
        ['max_response_size' => 0, 'block_private_ips' => false]
    );
    return true;
}

// /bytes/<size> (#201): serve a body of EXACTLY <size> '#' bytes — the
// deterministic size fixture for the max_response_size tests (the #169
// servers/apache/tests/server.py / tests/server/main.go /bytes generator,
// reduced to this suite's needs). Guarded against absurd allocations.
if (preg_match('#^/bytes/([0-9]+)$#', $path, $bytes_m)) {
    $n = (int)$bytes_m[1];
    if ($n < 0 || $n > 67108864) {
        return false;
    }
    header('Content-Type: application/octet-stream');
    echo str_repeat('#', $n);
    return true;
}

if ($path === '/health') {
    header('Content-Type: text/plain');
    echo 'OK';
    return true;
}

return false;
