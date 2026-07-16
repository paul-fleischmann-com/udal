package mqtt

import "testing"

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
