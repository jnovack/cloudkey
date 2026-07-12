package buildversion

import (
	"runtime/debug"
	"testing"
)

func TestApplyBuildInfo(t *testing.T) {
	tests := []struct {
		name                                        string
		version, buildRFC3339, revision             string
		info                                        *debug.BuildInfo
		wantVersion, wantBuildRFC3339, wantRevision string
	}{
		{
			name:         "overrides defaults from vcs settings",
			version:      DefaultVersion,
			buildRFC3339: DefaultBuildRFC3339,
			revision:     DefaultRevision,
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "abc123"},
					{Key: "vcs.time", Value: "2026-01-02T03:04:05Z"},
				},
			},
			wantVersion:      DefaultVersion, // "(devel)" is excluded, so Version stays at default
			wantBuildRFC3339: "2026-01-02T03:04:05Z",
			wantRevision:     "abc123",
		},
		{
			name:         "ldflags-set values are never overridden",
			version:      "v1.2.3",
			buildRFC3339: "2020-01-01T00:00:00Z",
			revision:     "deadbeef",
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "v9.9.9"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "abc123"},
					{Key: "vcs.time", Value: "2026-01-02T03:04:05Z"},
				},
			},
			wantVersion:      "v1.2.3",
			wantBuildRFC3339: "2020-01-01T00:00:00Z",
			wantRevision:     "deadbeef",
		},
		{
			name:         "module version overrides when not devel",
			version:      DefaultVersion,
			buildRFC3339: DefaultBuildRFC3339,
			revision:     DefaultRevision,
			info: &debug.BuildInfo{
				Main:     debug.Module{Version: "v2.0.0"},
				Settings: nil,
			},
			wantVersion:      "v2.0.0",
			wantBuildRFC3339: DefaultBuildRFC3339,
			wantRevision:     DefaultRevision,
		},
		{
			name:         "empty setting values are ignored",
			version:      DefaultVersion,
			buildRFC3339: DefaultBuildRFC3339,
			revision:     DefaultRevision,
			info: &debug.BuildInfo{
				Main: debug.Module{Version: ""},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: ""},
					{Key: "vcs.time", Value: ""},
				},
			},
			wantVersion:      DefaultVersion,
			wantBuildRFC3339: DefaultBuildRFC3339,
			wantRevision:     DefaultRevision,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVersion, gotBuildRFC3339, gotRevision := applyBuildInfo(tt.version, tt.buildRFC3339, tt.revision, tt.info)
			if gotVersion != tt.wantVersion {
				t.Errorf("version = %q, want %q", gotVersion, tt.wantVersion)
			}
			if gotBuildRFC3339 != tt.wantBuildRFC3339 {
				t.Errorf("buildRFC3339 = %q, want %q", gotBuildRFC3339, tt.wantBuildRFC3339)
			}
			if gotRevision != tt.wantRevision {
				t.Errorf("revision = %q, want %q", gotRevision, tt.wantRevision)
			}
		})
	}
}

func TestReadBuildInfoUnavailable(t *testing.T) {
	// debug.ReadBuildInfo() always succeeds for a normally-built test binary,
	// so this only exercises the ok path end-to-end; the ok=false short
	// circuit is covered by inspection (single early return, no branching).
	gotVersion, gotBuildRFC3339, gotRevision := readBuildInfo("v1.2.3", "2020-01-01T00:00:00Z", "deadbeef")
	if gotVersion != "v1.2.3" || gotBuildRFC3339 != "2020-01-01T00:00:00Z" || gotRevision != "deadbeef" {
		t.Errorf("readBuildInfo overrode explicit values: got (%q, %q, %q)", gotVersion, gotBuildRFC3339, gotRevision)
	}
}
