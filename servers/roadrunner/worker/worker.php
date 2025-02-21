<?php
include "vendor/autoload.php";

use Spiral\RoadRunner;
use Nyholm\Psr7;

$worker = RoadRunner\Worker::create();
$psrFactory = new Psr7\Factory\Psr17Factory();

$worker = new RoadRunner\Http\PSR7Worker($worker, $psrFactory, $psrFactory, $psrFactory);

const HTML = <<<'HTML'
<!DOCTYPE html>
<html lang="en">
<head>
    <title>Test ESI</title>
</head>
<body>
<!--esi <h1>Welcome to ESI Test</h1> -->
<esi:remove><h1>Failed to include ESI</h1></esi:remove>
</body>
</html>
HTML;


while ($req = $worker->waitRequest()) {
    try {
        $rsp = new Psr7\Response();
        $rsp->getBody()->write(HTML);
        $rsp = $rsp->withHeader('Content-Type', 'text/html');

        $worker->respond($rsp);
    } catch (\Throwable $e) {
        $worker->getWorker()->error((string)$e);
    }
}