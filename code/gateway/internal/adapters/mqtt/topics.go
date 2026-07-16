// Package mqtt implements the MQTT transport adapter (req42.adoc F-09,
// GitHub issue #11): devices exposed over MQTT are read/written via a
// request/response pattern over topics, rather than the direct-gRPC path
// devices using code/sdk/go use (StreamCommands).
//
// Topic convention:
//
//	udal/{deviceId}/props/{path}       device publishes value
//	udal/{deviceId}/props/{path}/get   gateway requests value
//	udal/{deviceId}/props/{path}/set   gateway writes value
//	udal/{deviceId}/props/{path}/set/ack  device confirms a write
//	udal/{deviceId}/cmds/{name}        gateway sends command (not wired up
//	                                   in this ticket — no acceptance
//	                                   criterion requires MQTT command
//	                                   dispatch; SendCommand only routes to
//	                                   directly-connected gRPC devices)
//	udal/{deviceId}/status             device heartbeat (not consumed yet)
package mqtt

import (
	"fmt"
	"strings"
)

func topicProps(deviceID, path string) string   { return fmt.Sprintf("udal/%s/props/%s", deviceID, path) }
func topicPropsWildcard(deviceID string) string { return fmt.Sprintf("udal/%s/props/#", deviceID) }
func topicGet(deviceID, path string) string     { return topicProps(deviceID, path) + "/get" }
func topicSet(deviceID, path string) string     { return topicProps(deviceID, path) + "/set" }
func topicSetAck(deviceID, path string) string  { return topicSet(deviceID, path) + "/ack" }

// parsePropsTopic extracts (deviceID, path) from a bare value-publish topic
// "udal/{deviceId}/props/{path}", rejecting the reserved request/ack
// sub-topics ("/get", "/set", "/set/ack") that share the same prefix under a
// wildcard "props/#" subscription.
func parsePropsTopic(topic string) (deviceID, path string, ok bool) {
	deviceID, rest, ok := parsePropsTopicOrSetAck(topic)
	if !ok {
		return "", "", false
	}
	if rest == "get" || strings.HasSuffix(rest, "/get") ||
		rest == "set" || strings.HasSuffix(rest, "/set") ||
		strings.HasSuffix(rest, "/set/ack") {
		return "", "", false
	}
	return deviceID, rest, true
}

// parsePropsTopicOrSetAck extracts (deviceID, rest) from any topic under
// "udal/{deviceId}/props/" — bare value-publish, /get, /set or /set/ack
// alike. Used where the caller only needs deviceID (e.g. checking whether a
// "props/#" wildcard subscription already covers a topic).
func parsePropsTopicOrSetAck(topic string) (deviceID, rest string, ok bool) {
	const prefix = "udal/"
	if !strings.HasPrefix(topic, prefix) {
		return "", "", false
	}
	deviceID, rest, ok = strings.Cut(topic[len(prefix):], "/props/")
	if !ok || deviceID == "" || rest == "" {
		return "", "", false
	}
	return deviceID, rest, true
}
