#!/usr/bin/env python3

import socket
import argparse

def start_server(port):
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
    parser = argparse.ArgumentParser(description="Simple UDP Server")
    parser.add_argument("port", type=int, help="Port to listen on")
    args = parser.parse_args()
    
    start_server(args.port)
