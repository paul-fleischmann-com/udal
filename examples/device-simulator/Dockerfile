# Simulated MQTT temperature-sensor device for the docker-compose
# demonstrator stack (issue #30) — publishes fake telemetry via
# mosquitto_pub, no gateway/broker code involved.
FROM alpine:3.20
RUN apk add --no-cache mosquitto-clients
COPY simulate.sh /simulate.sh
RUN chmod +x /simulate.sh
ENTRYPOINT ["/simulate.sh"]
