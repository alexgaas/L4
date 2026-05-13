#!/usr/bin/env python3

import socket
import argparse
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

class HealthCheckHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"OK")
    def log_message(self, format, *args):
        return

def start_http_server(port):
    server = HTTPServer(('0.0.0.0', port), HealthCheckHandler)
    print(f"Starting HTTP health check server on port {port}")
    server.serve_forever()

def start_udp_server(port):
    # Create a UDP socket
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    
    # Bind the socket to the port
    server_address = ('0.0.0.0', port)
    print(f"Starting UDP server on {server_address[0]} port {server_address[1]}")
    sock.bind(server_address)

    while True:
        data, address = sock.recvfrom(65535)
        
        try:
            text = data.decode('utf-8')
            print(f"Received {len(data)} bytes from {address}: {text}", flush=True)
        except UnicodeDecodeError:
            print(f"Received {len(data)} bytes from {address} (binary data)", flush=True)

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="UDP Server with HTTP Health Check")
    parser.add_argument("port", type=int, help="UDP port to listen on")
    parser.add_argument("--http-port", type=int, default=8080, help="HTTP health check port (default: 8080)")
    args = parser.parse_args()
    
    # Start HTTP server in a separate thread
    http_thread = threading.Thread(target=start_http_server, args=(args.http_port,), daemon=True)
    http_thread.start()
    
    start_udp_server(args.port)
