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
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidTopicSegment is returned by ReadProperty/WriteProperty/
// WatchDevice when deviceID or path contains a "/"-separated level that is
// exactly "+" or "#" — the two MQTT wildcard characters, which only act as
// wildcards when they make up an entire topic level on their own (a level
// like "dev+1" is just a literal string to a broker). Both values are
// ultimately caller-supplied and otherwise unvalidated
// (RegisterDeviceRequest.id, GetPropertyRequest.property_path — see
// device_service.go), yet used verbatim to build topics: a deviceID of
// "+", for example, would turn the per-device wildcard subscription
// "udal/{deviceId}/props/#" into "udal/+/props/#", matching every device on
// the broker rather than just this one.
var ErrInvalidTopicSegment = errors.New(`mqtt: device ID or property path must not contain a "/"-separated level that is exactly "+" or "#"`)

func validTopicSegment(s string) error {
	for _, level := range strings.Split(s, "/") {
		if level == "+" || level == "#" {
			return ErrInvalidTopicSegment
		}
	}
	return nil
}

func topicProps(deviceID, path string) string   { return fmt.Sprintf("udal/%s/props/%s", deviceID, path) }
func topicPropsWildcard(deviceID string) string { return fmt.Sprintf("udal/%s/props/#", deviceID) }
func topicGet(deviceID, path string) string     { return topicProps(deviceID, path) + "/get" }
func topicSet(deviceID, path string) string     { return topicProps(deviceID, path) + "/set" }
func topicSetAck(deviceID, path string) string  { return topicSet(deviceID, path) + "/ack" }
func topicStatus(deviceID string) string        { return fmt.Sprintf("udal/%s/status", deviceID) }

// parseStatusTopic extracts deviceID from a device heartbeat topic
// "udal/{deviceId}/status" (issue #42), rejecting anything else — notably
// it's a sibling of "props/", not covered by the props/# wildcard
// subscription, so it needs its own subscribe (see Adapter.WatchDevice)
// and its own dispatch check.
func parseStatusTopic(topic string) (deviceID string, ok bool) {
	const prefix, suffix = "udal/", "/status"
	if !strings.HasPrefix(topic, prefix) || !strings.HasSuffix(topic, suffix) {
		return "", false
	}
	deviceID = topic[len(prefix) : len(topic)-len(suffix)]
	if deviceID == "" || strings.Contains(deviceID, "/") {
		return "", false
	}
	return deviceID, true
}

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
