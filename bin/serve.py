#!/usr/bin/env python3

import http.server
import socketserver
import sys
import signal

def main():
    url = sys.argv[1]
    cookie_file = sys.argv[2]
    port = int(sys.argv[3]) if len(sys.argv) > 3 else 17777

    cookies = []
    with open(cookie_file) as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith('#'):
                continue
            parts = line.split('\t')
            if len(parts) >= 7:
                cookies.append((parts[5], parts[6]))

    class H(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(302)
            for name, value in cookies:
                self.send_header('Set-Cookie', f'{name}={value}; Path=/')
            self.send_header('Location', url)
            self.end_headers()
        def log_message(self, *a): pass

    # Allow quick rebinding to the port
    socketserver.TCPServer.allow_reuse_address = True

    with socketserver.TCPServer(('', port), H) as server:
        def shutdown(sig, frame):
            server.shutdown()
            sys.exit(0)

        signal.signal(signal.SIGTERM, shutdown)
        signal.signal(signal.SIGINT, shutdown)

        server.handle_request()

if __name__ == '__main__':
    main()
