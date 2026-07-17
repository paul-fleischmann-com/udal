package capability

import "fmt"

// semver is a parsed "major.minor.patch" version (the only format
// schema/udal-capability.schema.json's metadata.version pattern allows).
type semver struct{ major, minor, patch int }

func parseSemver(s string) (semver, error) {
	var v semver
	n, err := fmt.Sscanf(s, "%d.%d.%d", &v.major, &v.minor, &v.patch)
	if err != nil || n != 3 {
		return semver{}, fmt.Errorf("capability: invalid semver %q", s)
	}
	return v, nil
}

func (v semver) less(o semver) bool {
	if v.major != o.major {
		return v.major < o.major
	}
	if v.minor != o.minor {
		return v.minor < o.minor
	}
	return v.patch < o.patch
}

// detectBreakingChanges compares old against new and returns a
// human-readable description of each apparent breaking change: a removed
// property/command, a property/parameter whose type changed, or a removed
// command parameter. Added properties/commands are never breaking. This is
// a pragmatic heuristic (issue #22's "warn on breaking changes between
// minor versions"), not an exhaustive compatibility checker.
func detectBreakingChanges(old, new Schema) []string {
	var issues []string

	for name, oldProp := range old.Properties {
		newProp, ok := new.Properties[name]
		if !ok {
			issues = append(issues, fmt.Sprintf("property %q removed", name))
			continue
		}
		if oldProp.Type != newProp.Type {
			issues = append(issues, fmt.Sprintf("property %q type changed from %q to %q", name, oldProp.Type, newProp.Type))
		}
	}

	for name, oldCmd := range old.Commands {
		newCmd, ok := new.Commands[name]
		if !ok {
			issues = append(issues, fmt.Sprintf("command %q removed", name))
			continue
		}
		for pname, oldParam := range oldCmd.Params {
			newParam, ok := newCmd.Params[pname]
			if !ok {
				issues = append(issues, fmt.Sprintf("command %q parameter %q removed", name, pname))
				continue
			}
			if oldParam.Type != newParam.Type {
				issues = append(issues, fmt.Sprintf("command %q parameter %q type changed from %q to %q", name, pname, oldParam.Type, newParam.Type))
			}
		}
	}

	return issues
}

// warnIfBreaking logs (at Warn level) if latest and next share the same
// major version but next appears to remove/retype something latest
// declared — a minor/patch bump that isn't actually backward compatible.
// A major-version bump is allowed to break compatibility, so this only
// fires within the same major version.
func warnIfBreaking(log breakingChangeLogger, latest, next Schema) {
	latestVer, err := parseSemver(latest.Version)
	if err != nil {
		return
	}
	nextVer, err := parseSemver(next.Version)
	if err != nil {
		return
	}
	if latestVer.major != nextVer.major || !latestVer.less(nextVer) {
		return
	}
	if issues := detectBreakingChanges(latest, next); len(issues) > 0 {
		log.Warn("capability: potentially breaking change between minor/patch versions",
			"name", next.Name, "from", latest.Version, "to", next.Version, "issues", issues)
	}
}

// breakingChangeLogger is the one *slog.Logger method warnIfBreaking needs
// — narrow on purpose so both registry implementations can share it without
// importing slog into this file's public surface.
type breakingChangeLogger interface {
	Warn(msg string, args ...any)
}
