<?php
$path = parse_url($_SERVER['REQUEST_URI'], PHP_URL_PATH);
$backend = getenv('MESI_BACKEND_URL') ?: 'http://test-server/';
$esiIncludeUrl = rtrim($backend, '/') . '/esi';

// Helper for the max_concurrent_requests fixtures (#206): reset the
// test-server's peak tracker before a parse (function declarations are
// per-request, safe to redeclare across built-in-server requests).
function mcr_reset($backend) {
    @file_get_contents(rtrim($backend, '/') . '/track/reset');
}

// 20 includes, one DISTINCT /hold label each, so every include reaches
// the backend's peak-concurrency counter.
function mcr_input($backend, $label) {
    $base = rtrim($backend, '/') . '/hold/1500/' . $label;
    $input = '<p>MCR-TEST</p>';
    for ($i = 1; $i <= 20; $i++) {
        $input .= '<esi:include src="' . $base . '-' . $i . '" />';
    }
    return $input;
}

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

// max_concurrent_requests (#206): 20 x 1500 ms /hold includes against the
// test-server (Go, CONCURRENT — a single-threaded php -S backend would
// serialize every hold into a peak of 1 and prove nothing, which is why
// these fixtures must never be served by this process). Each parse route
// zeroes the backend's peak tracker first, so the /-peak control route
// reads the peak OF THAT parse (only /hold touches the tracker, so the
// other fixtures' traffic cannot pollute it; distinct label sets per
// route keep every include its own URL). block_private_ips=false so the
// loopback/container-IP dial is allowed; the timeout key stays ABSENT so
// the documented 30s default applies — well above the ~10.5 s worst case
// of 20 x 1500 ms queued 3 at a time (and the mcr-only blob then proves
// the per-key conditional rendering end to end).
if ($path === '/max-concurrent-requests-cap') {
    header('Content-Type: text/html');
    mcr_reset($backend);
    echo \mesi\parse_with_config(
        mcr_input($backend, 'mcr-cap'),
        5,
        $backend,
        ['max_concurrent_requests' => 3, 'block_private_ips' => false]
    );
    return true;
}

// explicit 0: the documented "unlimited" value (the core only installs
// the admission semaphore when MaxConcurrentRequests > 0) — the fan-out
// must NOT be throttled (peak >= 4, same bound as the CLI #192 tests:
// the worker pool is min(NumCPU*4, 20) >= 4 goroutines).
if ($path === '/max-concurrent-requests-zero') {
    header('Content-Type: text/html');
    mcr_reset($backend);
    echo \mesi\parse_with_config(
        mcr_input($backend, 'mcr-zero'),
        5,
        $backend,
        ['max_concurrent_requests' => 0, 'block_private_ips' => false]
    );
    return true;
}

// key absent: documented default 0 = UNLIMITED — the value every
// positional path leaves; the call must stay on the exact backward-
// compatible behaviour (fan-out peak >= 4).
if ($path === '/max-concurrent-requests-absent') {
    header('Content-Type: text/html');
    mcr_reset($backend);
    echo \mesi\parse_with_config(
        mcr_input($backend, 'mcr-absent'),
        5,
        $backend,
        ['block_private_ips' => false]
    );
    return true;
}

// Control endpoint: expose the test-server's recorded peak through this
// app — docker mode does not publish the test-server's port to the host,
// so test.sh must be able to read /track/max through the php-ext service
// (file_get_contents here runs in the outer request, no deadlock: the
// backend is the Go server, never this single-threaded process).
if ($path === '/max-concurrent-requests-peak') {
    header('Content-Type: text/plain');
    $peak = @file_get_contents(rtrim($backend, '/') . '/track/max');
    echo $peak === false ? 'unavailable' : $peak;
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
