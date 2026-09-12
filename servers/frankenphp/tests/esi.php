<?php
header('Content-Type: text/html');
?>
<!DOCTYPE html>
<html lang="en">
<head>
    <title>ESI PHP Test</title>
</head>
<body>
<h1>Welcome to ESI PHP Test</h1>
<esi:include src="http://test-server/esi" />
<esi:remove>Failed to include ESI from PHP</esi:remove>
<!--esi <p>PHP unwrapped content</p> -->
</body>
</html>
