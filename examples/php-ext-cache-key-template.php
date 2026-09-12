<?php
/**
 * Example: use the PHP mESI extension's parse_with_config() with
 * cache_key_template to vary cache keys by request header/cookie.
 *
 * Run from CLI:
 *    php examples/php-ext-cache-key-template.php
 *
 * Requires:
 *   - libgomesi built (`cd libgomesi && make`)
 *   - PHP extension built and loaded (`php -m | grep mesi`)
 *
 * Notes:
 *   - Unknown placeholders (e.g. ${unknown:foo}) stay literal.
 *   - Empty/absent template falls back to the default URL-only key
 *     (mesi.DefaultCacheKey).
 *   - When cache_backend is "" the template is silently ignored.
 */

declare(strict_types=1);

$input = <<<ESI
<header>
  <esi:include src="http://test-server/esi"/>
</header>
<main>
  <esi:include src="http://test-server/esi"/>
</main>
ESI;

$result = \mesi\parse_with_config(
    $input,
    5,
    'http://test-server/',
    [
        'cache_backend'      => 'memory',
        'cache_size'         => 1000,
        'cache_ttl'          => 60,
        'cache_key_template' => 'mesi:${url}:${header:Accept-Language}',
        'request_headers'    => ['Accept-Language' => 'pl'],
        // 'request_cookies' => ['ab' => 'v1'],  // also supported: ${cookie:ab}
    ]
);

if ($result === false) {
    fwrite(STDERR, "parse_with_config failed\n");
    exit(1);
}

echo $result;
