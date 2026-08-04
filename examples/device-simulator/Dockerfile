# Simulated MQTT temperature-sensor device for the docker-compose
# demonstrator stack (issue #30) — publishes fake telemetry via
# mosquitto_pub, no gateway/broker code involved.
FROM alpine:3.20
RUN apk add --no-cache mosquitto-clients
COPY simulate.sh /simulate.sh
# Strip any CRLF line endings before anything else: a #!/bin/sh\r shebang
# (e.g. from a Windows checkout with core.autocrlf not set to "input",
# even with .gitattributes present if the working tree was already
# checked out before it was added) makes the container look for an
# interpreter literally named "/bin/sh\r", which doesn't exist —
# surfaced as a misleading "exec ... missing dynamic library?" error.
RUN sed -i 's/\r$//' /simulate.sh && chmod +x /simulate.sh
ENTRYPOINT ["/simulate.sh"]
