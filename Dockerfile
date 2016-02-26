# HA-Redis
FROM redis:3-alpine

# Added start process
ADD ./ha-redis /app/

# Start the monitor
ENTRYPOINT ["/app/ha-redis"]
