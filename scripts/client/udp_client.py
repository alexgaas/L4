#!/usr/bin/env python3

import socket
import argparse

def send_message(host, port, message):
    # Create a UDP socket
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    
    server_address = (host, port)
    
    try:
        # Send data
        print(f"Sending '{message}' to {host}:{port}")
        sent = sock.sendto(message.encode('utf-8'), server_address)
    finally:
        sock.close()

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Simple UDP Client")
    parser.add_argument("--host", default="127.0.0.1", help="Target host (default: 127.0.0.1)")
    parser.add_argument("--port", type=int, default=2050, help="Target port (default: 2050)")
    parser.add_argument("--message", default="Hello, Balancer!", help="Message to send")
    args = parser.parse_args()
    
    send_message(args.host, args.port, args.message)
