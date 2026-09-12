from http.server import HTTPServer, BaseHTTPRequestHandler
import os

counter = 0
lang_counter = 0


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        global counter
        global lang_counter

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


HTTPServer(('0.0.0.0', 8000), Handler).serve_forever()
