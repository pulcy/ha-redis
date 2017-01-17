# HA-Redis
FROM redis:3-alpine

# Install haproxy
RUN apk add -U haproxy 
RUN mkdir -p /data/config

# Added start process
ADD ./ha-redis /app/

# This is where the metrics server listens on
EXPOSE 8080 
# This is where haproxy listens on (the external entrypoint)
EXPOSE 6379
# This is where the internal redis instance listens on
EXPOSE 6380 

# Start the monitor
ENTRYPOINT ["/app/ha-redis"]
