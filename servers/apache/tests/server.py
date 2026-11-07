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
#
# Also serves the MesiParseOnHeader (#172) endpoints:
# /parse-on-header/<esi|plain|nested> returns a checked-in HTML fixture,
# with `Edge-control: dca=esi` for "esi"/"nested" and without it for
# "plain"; /poh-nolen-html/<esi|plain>/<size> is the streamed, length-less
# body of /nolen-html with the same optional signal.
import os
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
        if len(parts) == 3 and parts[1] == 'parse-on-header':
            # /parse-on-header/<esi|plain|nested> (#172): a checked-in HTML
            # fixture with `Edge-control: dca=esi` for "esi"/"nested", and
            # without the header for "plain". The nested page includes both
            # fragments so the parser's ParseOnHeader flag decides whether
            # each fragment's own <esi:include> is expanded.
            modes = {
                'esi': ('parse-on-header.html', True),
                'plain': ('parse-on-header.html', False),
                'nested': ('parse-on-header-nested.html', True),
            }
            if parts[2] in modes:
                name, signalled = modes[parts[2]]
                # Resolve next to this script, not against the process CWD,
                # so a missing fixture is a loud 500 rather than an empty
                # 200 body that looks like a parsing bug.
                fixture = os.path.join(os.path.dirname(os.path.abspath(__file__)), name)
                try:
                    with open(fixture, 'rb') as fh:
                        body = fh.read()
                except OSError:
                    self.send_error(500, 'missing parse-on-header fixture')
                    return
                try:
                    self.send_response(200)
                    self.send_header('Content-Type', 'text/html')
                    if signalled:
                        self.send_header('Edge-control', 'dca=esi')
                    self.send_header('Content-Length', str(len(body)))
                    self.end_headers()
                    self.wfile.write(body)
                except (BrokenPipeError, ConnectionResetError):
                    pass
                return
        if len(parts) == 4 and parts[1] == 'poh-nolen-html':
            # /poh-nolen-html/<esi|plain>/<size> (#172): the streamed,
            # length-less body of /nolen-html with an optional ESI signal.
            # An unsignalled page must bypass MesiMaxBodySize entirely
            # (the filter passes it through before buffering).
            mode = parts[2]
            try:
                size = int(parts[3])
            except ValueError:
                size = -1
            if mode in ('esi', 'plain') and 0 <= size <= 1000000:
                try:
                    self.send_response(200)
                    self.send_header('Content-Type', 'text/html')
                    if mode == 'esi':
                        self.send_header('Edge-control', 'dca=esi')
                    self.end_headers()
                    self.wfile.write(b'<esi:include src="http://backend:8000/include.txt" />\n')
                    sent = 0
                    while sent < size:
                        self.wfile.write(b'x' * 500)
                        self.wfile.flush()
                        sent += 500
                    self.wfile.write(b'\n<p>nolen-end-marker</p>\n')
                except (BrokenPipeError, ConnectionResetError):
                    pass
                return
        if len(parts) >= 3 and parts[1] == 'nolen-html':
            # /nolen-html/<size>: an HTML page of about <size> bytes sent
            # in 500-byte writes WITHOUT Content-Length (HTTP/1.0, closed
            # by the server), so the Apache body limit has to count the
            # streamed brigades. Starts with an include tag and ends with
            # a marker. Used by the MesiMaxBodySize tests.
            try:
                size = int(parts[2])
            except ValueError:
                size = -1
            if 0 <= size <= 1000000:
                try:
                    self.send_response(200)
                    self.send_header('Content-Type', 'text/html')
                    self.end_headers()
                    self.wfile.write(b'<esi:include src="http://backend:8000/include.txt" />\n')
                    sent = 0
                    while sent < size:
                        self.wfile.write(b'x' * 500)
                        self.wfile.flush()
                        sent += 500
                    self.wfile.write(b'\n<p>nolen-end-marker</p>\n')
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
        if len(parts) == 2 and parts[1] == 'probe.txt':
            # /probe.txt (#97): a text/html response at a .txt URI. mod_mime
            # may guess text/plain from the proxy filename at insert_filter
            # time, so the filter must still be attached and the response
            # gate must judge the origin's real Content-Type.
            body = b'<esi:include src="http://backend:8000/include.txt" />\n'
            try:
                self.send_response(200)
                self.send_header('Content-Type', 'text/html')
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            except (BrokenPipeError, ConnectionResetError):
                pass
            return
        if len(parts) == 3 and parts[1] == 'echo-header':
            # /echo-header/<name> (#97): a text/plain body containing the
            # request header Apache forwarded to the origin. This proves
            # Surrogate-Capability travels edge -> origin, not to the client.
            value = self.headers.get(parts[2], '')
            body = value.encode('latin-1', 'replace')
            try:
                self.send_response(200)
                self.send_header('Content-Type', 'text/plain')
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            except (BrokenPipeError, ConnectionResetError):
                pass
            return
        if len(parts) == 3 and parts[1] == 'esi-status':
            # /esi-status/<code> (#97): an HTML body containing an
            # <esi:include> tag returned with a non-2xx status. The Apache
            # output filter is attached before a proxied status is known,
            # so it must leave 3xx/4xx/5xx bodies completely raw.
            try:
                code = int(parts[2])
            except ValueError:
                code = -1
            if code in (204, 301, 302, 303, 304, 307, 308,
                        400, 401, 403, 404, 500, 502):
                try:
                    self.send_response(code)
                    if code in (204, 304):
                        self.end_headers()
                        return
                    if 300 <= code < 400:
                        self.send_header('Location', '/index.html')
                    body = b'<esi:include src="http://backend:8000/include.txt" />\n'
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
