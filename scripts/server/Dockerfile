FROM python:3.9-slim

WORKDIR /app

COPY udp_server.py .

# The port will be passed as an argument to the script
# We'll use 2054 as a default in the ENTRYPOINT, but it can be overridden
ENTRYPOINT ["python", "udp_server.py"]
CMD ["2054"]
