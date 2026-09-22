# Custom test backend for the Apache integration suite (#167, #169).
#
# Replaces `python -m http.server 8000` (see docker-compose.yml) to add
# the slow /sleep/<seconds>/<label> endpoints the MesiTimeout tests need
# and the generated /bytes/<size> bodies the MesiMaxResponseSize tests
# need. Static file serving is inherited from SimpleHTTPRequestHandler, so
# behaviour stays equivalent to the previous command (GET/HEAD, mimetypes,
# directory listings, 404s), and request logging keeps the
# `GET /path HTTP/1.1` line format that test.sh greps for cache counters.
# ThreadingHTTPServer keeps the container healthcheck and static fetches
# responsive while a /sleep request blocks its own thread
# (python -m http.server has used ThreadingHTTPServer since 3.7 anyway).
#
# Mirrors tests/server/main.go's /sleep/<timeout>/<index> endpoint:
# sleep first, then return a "<label> Waited <seconds>" fragment body.
import time
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer

# Hard cap for generated bodies (256 MB) — enough for the 50 MB
# MesiMaxResponseSize "unlimited" case while keeping a hostile URL
# from exhausting the container's memory.
MAX_GENERATED_BYTES = 268435456


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
