from http.server import HTTPServer, BaseHTTPRequestHandler
import os

counter = 0


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        global counter

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
