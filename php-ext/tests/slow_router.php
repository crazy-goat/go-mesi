<?php
// Slow fragment server for the #181 `timeout` tests (spawned by
// php-ext/test.sh as a DEDICATED `php -S` instance — both in CI on the
// runner and inside the php-ext container in docker mode, always on
// 127.0.0.1:18081). Every request sleeps 4s before answering, so:
//   - timeout=2 must cut the include at ~2s (SLOW-FRAGMENT absent),
//   - timeout=30 / an absent key (default 30s) must succeed (~4s).
sleep(4);
header('Content-Type: text/plain');
echo 'SLOW-FRAGMENT';
