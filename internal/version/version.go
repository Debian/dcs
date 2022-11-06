package version

import "runtime/debug"

type StructuredVersion struct {
	Revision string
	Modified bool
}

func (sv StructuredVersion) String() string {
	if len(sv.Revision) > 7 {
		sv.Revision = sv.Revision[:7]
	}
	modifiedSuffix := ""
	if sv.Modified {
		modifiedSuffix = " (modified)"
	}
	return sv.Revision + modifiedSuffix
}

func Read() StructuredVersion {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return StructuredVersion{Revision: "<ReadBuildInfo() failed>"}
	}
	settings := make(map[string]string)
	for _, s := range info.Settings {
		settings[s.Key] = s.Value
	}
	return StructuredVersion{
		Revision: settings["vcs.revision"],
		Modified: settings["vcs.modified"] == "true",
	}
}
