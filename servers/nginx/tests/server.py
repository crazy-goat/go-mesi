from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import os
import threading
import time

# Hard cap for generated bodies (256 MB) — enough for the 50 MB
# max_response_size "unlimited" case while keeping a hostile URL
# from exhausting the container's memory (mirrors Apache's
# tests/server.py cap, #169).
MAX_GENERATED_BYTES = 268435456

counter = 0
lang_counter = 0

# Peak-concurrency tracker for the mesi_max_concurrent_requests tests
# (#214). Deterministic observable counter instead of a wall-clock
# assertion: /hold/<millis>/<label> increments `current` (guarded by
# TRACK_LOCK) BEFORE sleeping, records the peak, sleeps, then
# decrements — so `peak` is the maximum number of /hold requests that
# had STARTED-but-not-finished at any one time. test.sh resets the
# counters, loads a page whose <esi:include> fan-out hits /hold, then
# reads the peak. Only /hold requests touch these counters and each
# test uses a distinct set of labels (the process-wide shared
# libgomesi cache would otherwise swallow repeat fetches), so
# healthcheck/other traffic cannot pollute the reading. Mirrors
# Apache's tests/server.py tracker (#170).
TRACK_LOCK = threading.Lock()
TRACK_CURRENT = [0]
TRACK_PEAK = [0]


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        global counter
        global lang_counter

        if self.path.startswith('/sleep/'):
            # /sleep/<seconds>/<label> (#184): blocks for <seconds>,
            # then returns a "<label> Waited <seconds>" fragment body —
            # the slow include the mesi_timeout tests need. Mirrors
            # Apache's tests/server.py /sleep endpoint (#167).
            parts = self.path.split('?', 1)[0].split('/')
            secs = -1
            label = 'sleep'
            if len(parts) >= 4:
                label = parts[3]
                try:
                    secs = int(parts[2])
                except ValueError:
                    secs = -1
            if 0 <= secs <= 3600:
                time.sleep(secs)
                body = (label + ' Waited ' + str(secs)).encode()
                self.send_response(200)
                self.send_header('Content-Type', 'text/html')
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                try:
                    self.wfile.write(body)
                except (BrokenPipeError, ConnectionResetError):
                    # Expected in the timeout tests: the ESI fetch
                    # budget can expire while this response is being
                    # written (nginx aborted the connection at
                    # mesi_timeout).
                    pass
                return

        if self.path.startswith('/bytes/'):
            # /bytes/<size> (#208): exactly <size> bytes, prefixed with
            # a "MesiBytesPayload <size>" marker line when the size
            # leaves room for it, so test.sh can prove the whole
            # fragment arrived (grep marker + wc -c) or was rejected
            # (marker absent). A checked-in static fixture cannot
            # express "500 KB" / "50 MB" without bloating the repo,
            # hence generation — mirrors Apache's tests/server.py
            # /bytes endpoint (#169). Invalid or oversized requests
            # fall through to the static handler (404).
            parts = self.path.split('?', 1)[0].split('/')
            try:
                size = int(parts[2]) if len(parts) >= 3 else -1
            except ValueError:
                size = -1
            if 0 <= size <= MAX_GENERATED_BYTES:
                marker = ('MesiBytesPayload ' + parts[2] + '\n').encode()
                if size >= len(marker):
                    body = marker + b'x' * (size - len(marker))
                else:
                    body = b'x' * size
                self.send_response(200)
                self.send_header('Content-Type', 'text/html')
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                try:
                    self.wfile.write(body)
                except (BrokenPipeError, ConnectionResetError):
                    # Expected in the size/timeout tests: the ESI fetch
                    # can fail or the budget can expire while this
                    # response is being written.
                    pass
                return

        if self.path.startswith('/hold/'):
            # /hold/<millis>/<label> (#214): track peak concurrency,
            # hold the request for <millis>, then return a
            # "<label> Held <millis>" fragment body. Distinct label
            # values give every include of a page its own URL (the
            # process-wide shared libgomesi cache would otherwise
            # dedup duplicates before they reach this counter).
            parts = self.path.split('?', 1)[0].split('/')
            ms = -1
            label = 'hold'
            if len(parts) >= 4:
                label = parts[3]
                try:
                    ms = int(parts[2])
                except ValueError:
                    ms = -1
            if 0 <= ms <= 60000:
                with TRACK_LOCK:
                    TRACK_CURRENT[0] += 1
                    if TRACK_CURRENT[0] > TRACK_PEAK[0]:
                        TRACK_PEAK[0] = TRACK_CURRENT[0]
                try:
                    time.sleep(ms / 1000.0)
                    body = (label + ' Held ' + str(ms)).encode()
                    self.send_response(200)
                    self.send_header('Content-Type', 'text/html')
                    self.send_header('Content-Length', str(len(body)))
                    self.end_headers()
                    try:
                        self.wfile.write(body)
                    except (BrokenPipeError, ConnectionResetError):
                        # The ESI fetch budget can expire while this
                        # response is being written (nginx aborted the
                        # connection at mesi_timeout).
                        pass
                finally:
                    with TRACK_LOCK:
                        TRACK_CURRENT[0] -= 1
                return

        if self.path == '/track/reset':
            # Control endpoint for test.sh (#214), proxied through the
            # nginx /track/ location. text/plain.
            with TRACK_LOCK:
                TRACK_CURRENT[0] = 0
                TRACK_PEAK[0] = 0
            body = b'reset'
            self.send_response(200)
            self.send_header('Content-Type', 'text/plain')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return

        if self.path == '/track/max':
            # Peak in-flight /hold count since the last reset (#214).
            with TRACK_LOCK:
                body = str(TRACK_PEAK[0]).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'text/plain')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return

        if self.path == '/count':
            counter += 1
            count = counter
            body = str(count).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'text/plain')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return

        if self.path == '/langcount':
            # Cache-key-template fixture endpoint: a fresh number per fetch.
            # The ESI include fetch does not forward the parent request's
            # headers, so this counter stands in for the language-specific
            # variant content a real backend would serve — each cache key
            # variant pins the number its own first fetch produced.
            lang_counter += 1
            count = lang_counter
            body = str(count).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'text/plain')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return

        if self.path == '/redirect-to-cdn':
            self.send_response(302)
            self.send_header('Location',
                             'http://cdn.example.net:8000/redirect_target.html')
            self.send_header('Content-Length', '0')
            self.end_headers()
            return

        path = self.path.lstrip('/')
        if os.path.isfile(path):
            with open(path, 'rb') as f:
                content = f.read()
            self.send_response(200)
            self.send_header('Content-Type', 'text/plain')
            self.send_header('Content-Length', str(len(content)))
            self.end_headers()
            self.wfile.write(content)
            return

        self.send_response(404)
        self.send_header('Content-Length', '0')
        self.end_headers()


# ThreadingHTTPServer keeps the container healthcheck and other static
# fetches responsive while a /sleep request blocks its own thread (the
# aborted 2s fetch's backend thread keeps sleeping out its 5s, so a
# sequential server would queue later includes behind it) — same
# rationale as Apache's tests/server.py.
ThreadingHTTPServer(('0.0.0.0', 8000), Handler).serve_forever()
