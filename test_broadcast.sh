#!/bin/bash

# test_broadcast.sh
# Test scenario for L4 Balancer UDP Broadcasting

# Function to cleanup on exit
cleanup() {
    echo ""
    echo "Cleaning up..."
    kill $P1 $P2 $P3 2>/dev/null
    if [ ! -z "$BALANCER_PID" ]; then
        sudo kill $BALANCER_PID 2>/dev/null
    fi
    rm -f server_*.log
    echo "Done."
    exit
}

trap cleanup SIGINT SIGTERM EXIT

# 1. Build the balancer
echo "Building balancer..."
(cd balancer && go build -o balancer main.go)
if [ $? -ne 0 ]; then
    echo "Failed to build balancer."
    exit 1
fi

# 2. Start UDP servers in background
echo "Starting 3 UDP servers on ports 2054, 2055, 2056..."
python3 scripts/server/udp_server.py 2054 > server_2054.log 2>&1 &
P1=$!
python3 scripts/server/udp_server.py 2055 > server_2055.log 2>&1 &
P3=$!
python3 scripts/server/udp_server.py 2056 > server_2056.log 2>&1 &
P2=$!

# Give servers a moment to start
sleep 1

# 3. Start the balancer
echo "--------------------------------------------------------"
echo "The L4 balancer requires root privileges for RAW sockets."
echo "Starting balancer (config: balancer/config.yaml)..."
echo "--------------------------------------------------------"
# Use -u to prevent buffering issues if possible, though not applicable to go binary
# Run in background and redirect to file
sudo ./balancer/balancer -config balancer/config.yaml -verbose > balancer.log 2>&1 &
BALANCER_PID=$!

# Give balancer a moment to start and check if it's running
sleep 3
if ! ps -p $BALANCER_PID > /dev/null; then
    echo "Balancer failed to start. Check balancer.log"
    cat balancer.log
    cleanup
fi

# 4. Run the client
echo "Sending broadcast message via UDP client to port 2050..."
python3 scripts/client/udp_client.py --port 2050 --message "L4 Broadcast Message"

# Wait for packets to be processed
sleep 2

# 5. Check results
echo "--------------------------------------------------------"
echo "Balancer Logs:"
echo "--------------------------------------------------------"
cat balancer.log
echo "--------------------------------------------------------"
echo "Server Results:"
echo "--------------------------------------------------------"
grep "Received" server_*.log || echo "No messages received by servers."
echo "--------------------------------------------------------"

echo "Test scenario finished."
cleanup
