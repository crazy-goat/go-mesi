<?php
header('Content-Type: application/json');
echo json_encode([
    'message' => 'ESI test',
    'content' => '<esi:include src="http://test-server/esi" />'
]);
