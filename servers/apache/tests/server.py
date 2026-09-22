# Custom test backend for the Apache integration suite (#167, #169, #170).
#
# Replaces `python -m http.server 8000` (see docker-compose.yml) to add
# the slow /sleep/<seconds>/<label> endpoints the MesiTimeout tests need,
# the generated /bytes/<size> bodies the MesiMaxResponseSize tests
# need, and the /hold/<millis>/<label> + /track/* peak-concurrency
# endpoints the MesiMaxConcurrentRequests tests need. Static file
# serving is inherited from SimpleHTTPRequestHandler, so behaviour stays
# equivalent to the previous command (GET/HEAD, mimetypes, directory
# listings, 404s), and request logging keeps the
# `GET /path HTTP/1.1` line format that test.sh greps for cache counters.
# ThreadingHTTPServer keeps the container healthcheck and static fetches
# responsive while a /sleep or /hold request blocks its own thread
# (python -m http.server has used ThreadingHTTPServer since 3.7 anyway).
#
# Mirrors tests/server/main.go's /sleep/<timeout>/<index> endpoint:
# sleep first, then return a "<label> Waited <seconds>" fragment body.
import threading
import time
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer

# Hard cap for generated bodies (256 MB) — enough for the 50 MB
# MesiMaxResponseSize "unlimited" case while keeping a hostile URL
# from exhausting the container's memory.
MAX_GENERATED_BYTES = 268435456

# Peak-concurrency tracker for the MesiMaxConcurrentRequests tests
# (#170). Deterministic observable counter instead of a wall-clock
# assertion: /hold/<millis>/<label> increments `current` (guarded by
# TRACK_LOCK) BEFORE sleeping, records the peak, sleeps, then
# decrements — so `peak` is the maximum number of /hold requests that
# had STARTED-but-not-finished at any one time. test.sh resets the
# counters, loads a page whose <esi:include> fan-out hits /hold, then
# reads the peak. Only /hold requests touch these counters and each
# test uses a fresh set of labels (the in-process cache would otherwise
# swallow repeat fetches), so healthcheck/other traffic cannot pollute
# the reading.
TRACK_LOCK = threading.Lock()
TRACK_CURRENT = [0]
TRACK_PEAK = [0]


class Handler(SimpleHTTPRequestHandler):
    def do_GET(self):
        parts = self.path.split('?', 1)[0].split('/')
        if len(parts) >= 4 and parts[1] == 'sleep':
            try:
                secs = int(parts[2])
            except ValueError:
                secs = -1
            if 0 <= secs <= 3600:
                time.sleep(secs)
                body = (parts[3] + ' Waited ' + str(secs)).encode()
                try:
                    self.send_response(200)
                    self.send_header('Content-Type', 'text/html')
                    self.send_header('Content-Length', str(len(body)))
                    self.end_headers()
                    self.wfile.write(body)
                except (BrokenPipeError, ConnectionResetError):
                    # Expected in the timeout tests: the ESI fetch budget
                    # can expire while this response is being written
                    # (Apache aborted the connection at MesiTimeout).
                    pass
                return
        if len(parts) >= 4 and parts[1] == 'hold':
            # /hold/<millis>/<label> (#170): track peak concurrency,
            # hold the request for <millis>, then return a
            # "<label> Held <millis>" fragment body. Distinct label
            # values give every include of a page its own URL (the
            # Apache-side in-process cache would otherwise dedup
            # duplicates before they reach this counter).
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
                    body = (parts[3] + ' Held ' + str(ms)).encode()
                    try:
                        self.send_response(200)
                        self.send_header('Content-Type', 'text/html')
                        self.send_header('Content-Length', str(len(body)))
                        self.end_headers()
                        self.wfile.write(body)
                    except (BrokenPipeError, ConnectionResetError):
                        # The ESI fetch budget can expire while this
                        # response is being written.
                        pass
                finally:
                    with TRACK_LOCK:
                        TRACK_CURRENT[0] -= 1
                return
        if len(parts) >= 3 and parts[1] == 'track':
            # text/plain so the Apache ESI filter (html only) bypasses
            # these control endpoints when proxied via /backend/.
            if parts[2] == 'reset':
                with TRACK_LOCK:
                    TRACK_CURRENT[0] = 0
                    TRACK_PEAK[0] = 0
                body = b'reset'
            elif parts[2] == 'max':
                with TRACK_LOCK:
                    body = str(TRACK_PEAK[0]).encode()
            else:
                super().do_GET()
                return
            try:
                self.send_response(200)
                self.send_header('Content-Type', 'text/plain')
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            except (BrokenPipeError, ConnectionResetError):
                pass
            return
        if len(parts) >= 3 and parts[1] == 'bytes':
            # /bytes/<size> (#169): exactly <size> bytes, prefixed with a
            # "MesiBytesPayload <size>" marker line when the size leaves
            # room for it, so test.sh can prove the whole fragment arrived
            # (grep marker + wc -c) or was rejected (marker absent). A
            # checked-in static fixture cannot express "500 KB" / "50 MB"
            # without bloating the repo, hence generation. Invalid or
            # oversized requests fall through to the static handler (404).
            try:
                size = int(parts[2])
            except ValueError:
                size = -1
            if 0 <= size <= MAX_GENERATED_BYTES:
                marker = ('MesiBytesPayload ' + parts[2] + '\n').encode()
                if size >= len(marker):
                    body = marker + b'x' * (size - len(marker))
                else:
                    body = b'x' * size
                try:
                    self.send_response(200)
                    self.send_header('Content-Type', 'text/html')
                    self.send_header('Content-Length', str(len(body)))
                    self.end_headers()
                    self.wfile.write(body)
                except (BrokenPipeError, ConnectionResetError):
                    pass
                return
        super().do_GET()


if __name__ == '__main__':
    ThreadingHTTPServer(('0.0.0.0', 8000), Handler).serve_forever()
