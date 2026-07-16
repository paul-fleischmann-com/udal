package mqtt

import (
	"errors"
	"testing"
)

func TestTopicBuilders(t *testing.T) {
	if got, want := topicProps("dev-1", "temperature"), "udal/dev-1/props/temperature"; got != want {
		t.Errorf("topicProps = %q, want %q", got, want)
	}
	if got, want := topicPropsWildcard("dev-1"), "udal/dev-1/props/#"; got != want {
		t.Errorf("topicPropsWildcard = %q, want %q", got, want)
	}
	if got, want := topicGet("dev-1", "temperature"), "udal/dev-1/props/temperature/get"; got != want {
		t.Errorf("topicGet = %q, want %q", got, want)
	}
	if got, want := topicSet("dev-1", "temperature"), "udal/dev-1/props/temperature/set"; got != want {
		t.Errorf("topicSet = %q, want %q", got, want)
	}
	if got, want := topicSetAck("dev-1", "temperature"), "udal/dev-1/props/temperature/set/ack"; got != want {
		t.Errorf("topicSetAck = %q, want %q", got, want)
	}
}

func TestParsePropsTopic(t *testing.T) {
	cases := []struct {
		topic        string
		wantDeviceID string
		wantPath     string
		wantOK       bool
	}{
		{"udal/dev-1/props/temperature", "dev-1", "temperature", true},
		{"udal/dev-1/props/sensor/temperature", "dev-1", "sensor/temperature", true}, // nested path
		{"udal/dev-1/props/temperature/get", "", "", false},
		{"udal/dev-1/props/temperature/set", "", "", false},
		{"udal/dev-1/props/temperature/set/ack", "", "", false},
		{"udal/dev-1/cmds/calibrate", "", "", false}, // not a props topic at all
		{"not-udal/dev-1/props/temperature", "", "", false},
		{"udal//props/temperature", "", "", false}, // empty deviceID
	}
	for _, c := range cases {
		deviceID, path, ok := parsePropsTopic(c.topic)
		if ok != c.wantOK || deviceID != c.wantDeviceID || path != c.wantPath {
			t.Errorf("parsePropsTopic(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.topic, deviceID, path, ok, c.wantDeviceID, c.wantPath, c.wantOK)
		}
	}
}

func TestValidTopicSegment(t *testing.T) {
	// '+'/'#' only act as MQTT wildcards when they make up an entire
	// "/"-separated topic level on their own — a level like "dev+1" is
	// just a literal string to a broker, not a wildcard, so it must stay
	// valid (rejecting it would be stricter than the actual risk and could
	// reject legitimate device IDs/paths for no security benefit).
	valid := []string{"dev-1", "temperature", "sensor/temperature", "", "dev+1", "dev-#1", "a+b/c#d"}
	for _, s := range valid {
		if err := validTopicSegment(s); err != nil {
			t.Errorf("validTopicSegment(%q) = %v, want nil", s, err)
		}
	}

	invalid := []string{"+", "#", "sensor/+/temperature", "sensor/#", "udal/+/props/#"}
	for _, s := range invalid {
		if err := validTopicSegment(s); !errors.Is(err, ErrInvalidTopicSegment) {
			t.Errorf("validTopicSegment(%q) = %v, want ErrInvalidTopicSegment", s, err)
		}
	}
}

func TestParsePropsTopicOrSetAck(t *testing.T) {
	cases := []struct {
		topic        string
		wantDeviceID string
		wantOK       bool
	}{
		{"udal/dev-1/props/temperature", "dev-1", true},
		{"udal/dev-1/props/temperature/get", "dev-1", true},
		{"udal/dev-1/props/temperature/set/ack", "dev-1", true},
		{"udal/dev-1/cmds/calibrate", "", false},
	}
	for _, c := range cases {
		deviceID, _, ok := parsePropsTopicOrSetAck(c.topic)
		if ok != c.wantOK || deviceID != c.wantDeviceID {
			t.Errorf("parsePropsTopicOrSetAck(%q) = (%q, _, %v), want (%q, _, %v)",
				c.topic, deviceID, ok, c.wantDeviceID, c.wantOK)
		}
	}
}
