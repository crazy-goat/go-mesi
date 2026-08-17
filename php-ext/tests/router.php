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

if ($path === '/health') {
    header('Content-Type: text/plain');
    echo 'OK';
    return true;
}

return false;
