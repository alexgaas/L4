## L4 load balancer

The L4 Balancer is a high-performance UDP traffic management tool designed for transparent packet duplication and load balancing at the transport layer.

**Core Functionality**
* UDP Broadcasting (Duplication): Clones incoming UDP packets and simultaneously forwards them to multiple backend targets.
* L4 Load Balancing: Distributes traffic across backend clusters using Round Robin (RR) or Rendezvous Hashing (RH) algorithms.
* Source IP Preservation: Leverages RAW sockets (SOCK_RAW) to maintain the original client’s IP address, ensuring backends see the true source of the traffic.

**Key Technical Features**
* Platform-Specific Optimization: Implements high-performance IO using kqueue on Darwin (macOS) and epoll with sendmmsg/recvmmsg on Linux for low-latency packet processing.
* Health Monitoring: Includes an integrated health checker that monitors backend availability via HTTP and a status server for real-time monitoring.
* Dynamic Weighting: Supports weight-based balancing with a "weights file" that allows for real-time traffic distribution adjustments without restarting the service.
* Cross-Platform RAW Socket Handling: Features a custom network stack implementation that handles platform-specific IP header quirks (e.g., host vs. network byte order differences between Linux and BSD/Darwin).

#### Config example:
```yaml
status_port: 8443
servers:
  - port: 2050
    broadcaster:
      - backend:
          addr: '127.0.0.1:2054'
          preserve_src_addr: true
          fake_src_ipv4_addr: '95.108.254.95'
          uuid: 'backend-1'
          weight: 1.0
      - backend:
          addr: '127.0.0.1:2055'
          preserve_src_addr: true
          fake_src_ipv4_addr: '95.108.254.95'
          uuid: 'backend-2'
          weight: 1.0
      - backend:
          addr: '127.0.0.1:2056'
          preserve_src_addr: true
          fake_src_ipv4_addr: '95.108.254.95'
          uuid: 'backend-3'
          weight: 1.0
```

#### Weights config example:
```yaml
backend-1 10.0
backend-2 5.0
backend-3 1.0
```


**How it works:**
* Startup: The balancer reads config.yaml and starts with the weights defined there.
* Dynamic Update: When the balancer detects the weights file, it reads the new values and updates the internal routing table (e.g., in a Round Robin scenario, backend-1 will now receive 10x more traffic than backend-3).
* No Restart Required: You can modify the balancer/weights file at any time to rebalance traffic on the fly.

----

### Manual test (Darwin)

#### Build the balancer:

```go
cd balancer && go build -o balancer main.go && cd ..
```

#### Start the UDP Servers (in separate terminals):
```python
python3 scripts/server/udp_server.py 2054
python3 scripts/server/udp_server.py 2055
python3 scripts/server/udp_server.py 2056
```

#### Start the Balancer (requires root):
```shell
sudo ./balancer/balancer -config balancer/config.yaml
```

#### Send the message:
```python
python3 scripts/client/udp_client.py --port 2050 --message "Hello Balancer"
```

#### Output

Each of the three **UDP servers** will log the received message:
```shell
Received 14 bytes from ('127.0.0.1', <client_port>): Hello Balancer
```

----

### Tags
- **Golang** 1.25
- **epoll** support for **linux**, **kqueue** for **darwin**
- Tested manually on **MacOS** (darwin), within a Docker (Docker Desktop for a Mac) on the **linux**
- Built with Jetbrains **Goland** IDE

### References
- L7 load balancer as an initial source for a balancing algorithms - https://github.com/alexgaas/L7
- Priority queue (based on examples from): https://golang.org/pkg/container/heap/
- EDF implementation: https://en.wikipedia.org/wiki/Earliest_deadline_first_scheduling

----

### Docker-based manual test (linux) 

IN PROGRESS

----

### NOTE - how to troubleshoot OS-specific low-level network issues with tcpdump

To troubleshoot the packet delivery issue, I used **tcpdump** to perform low-level packet inspection on the _loopback_ **(lo0)** and _physical_ **(en0)** interfaces. 
This was critical in identifying that while the balancer claimed to be "sending" data, the OS was either dropping the packets or they were malformed.

I used the following command to capture all UDP traffic involved in the test (the entry port 2050 and the backend ports 2054-2056):

`sudo tcpdump -i lo0 -n -vvv udp port 2050 or udp port 2054 or udp port 2055 or udp port 2056`
* _-i lo0_: Listen on the loopback interface.
* _-n_: Don't resolve hostnames (shows IPs).
* _-vvv_: Verbose output (shows TTL, ID, Length, and Checksums).

The output revealed the tcpdump results provided the "smoking gun" for the Darwin-specific bug:
```shell
10:25:29.780997 IP (tos 0x0, ttl 64, id 30027, offset 0, flags [none], proto UDP (17), length 48, bad cksum 0 (->770)!)
127.0.0.1.53088 > 127.0.0.1.2060: [bad udp cksum 0xfe2f -> 0xb29f!] UDP, length 20
```

* Bad Checksum (bad cksum 0): This indicated that the IPv4 header checksum was incorrect or missing. On Darwin, when using IP_HDRINCL, the kernel expects a valid checksum unless you follow its specific byte-order rules.
* Packet Length: By comparing the length 48 reported by tcpdump with the actual data sent, I could see if the length field was being interpreted correctly by the kernel.
* Byte Order Mismatch: The fact that tcpdump showed "bad cksum" even when the code calculated one suggested the kernel was misreading the header fields (like total length) because they were in Network Byte Order (Big Endian) instead of Host Byte Order (Little Endian).

After applying the fix (converting **ip_len** and **ip_off** to Host Byte Order), I used tcpdump again to confirm that:
- the packets were actually leaving the balancer.
- the IP and UDP headers were correctly formed.
- the backend servers started receiving the packets immediately after the headers matched the OS expectations.