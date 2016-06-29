# HA-Redis
FROM redis:3-alpine

# Added start process
ADD ./ha-redis /app/

EXPOSE 8080
EXPOSE 6379

# Start the monitor
ENTRYPOINT ["/app/ha-redis"]
