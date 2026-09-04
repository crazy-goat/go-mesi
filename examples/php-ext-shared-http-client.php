<?php
/**
 * Example: use the PHP mESI extension's parse_with_config() with
 * shared_http_client to control connection pooling.
 *
 * Run from CLI:
 *    php examples/php-ext-shared-http-client.php
 *
 * Requires:
 *   - libgomesi built (`cd libgomesi && make`)
 *   - PHP extension built and loaded (`php -m | grep mesi`)
 *
 * Notes:
 *   - shared_http_client defaults to true: all parses in this worker
 *     share one libgomesi http.Client (TCP/TLS connection pooling).
 *   - shared_http_client => false detaches the shared client; every
 *     include fetch builds its own client from the parse-time config
 *     (block_private_ips is then honoured per parse).
 *   - The setting is process-wide state: the last value wins until
 *     changed.
 */

declare(strict_types=1);

$input = <<<ESI
<main>
  <esi:include src="http://test-server/esi"/>
</main>
ESI;

// Shared client (default): pooled connections across parses.
echo \mesi\parse_with_config(
    $input,
    5,
    'http://edge.example.com/',
    ['shared_http_client' => true]
);

// Per-parse clients: no shared state, historical behaviour.
echo \mesi\parse_with_config(
    $input,
    5,
    'http://edge.example.com/',
    ['shared_http_client' => false]
);
