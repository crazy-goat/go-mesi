<?php
$path = parse_url($_SERVER['REQUEST_URI'], PHP_URL_PATH);

if ($path === '/') {
    header('Content-Type: text/html');
    echo \mesi\parse(
        '<!DOCTYPE html><html><body>'
        . '<h1>ESI PHP Extension Test</h1>'
        . '<esi:include src="http://test-server/esi" />'
        . '<esi:remove>Failed to include ESI</esi:remove>'
        . '<!--esi <p>Unwrapped content</p> -->'
        . '</body></html>',
        5,
        'http://test-server/'
    );
    return true;
}

if ($path === '/plain') {
    header('Content-Type: text/plain');
    echo \mesi\parse(
        'plain text with <esi:include src="http://test-server/esi" /> tags',
        5,
        'http://test-server/'
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
        'http://test-server/'
    );
    return true;
}

if ($path === '/remove') {
    header('Content-Type: text/html');
    echo \mesi\parse(
        '<p>keep this</p><esi:remove>remove this</esi:remove><p>also keep this</p>',
        5,
        'http://test-server/'
    );
    return true;
}

if ($path === '/health') {
    header('Content-Type: text/plain');
    echo 'OK';
    return true;
}

return false;
