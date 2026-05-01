package dashboard

import "runtime/debug"

// BinaryVersion returns a semver from Go module metadata when present, otherwise a short VCS revision or "dev".
func BinaryVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			return "devel-" + s.Value[:7]
		}
	}
	return "dev"
}
