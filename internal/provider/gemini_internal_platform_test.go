package provider

import "testing"

func TestResolveCodeAssistPlatformFor(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		goarch string
		want   string
	}{
		{
			name:   "darwin amd64",
			goos:   "darwin",
			goarch: "amd64",
			want:   "MACOS",
		},
		{
			name:   "darwin arm64",
			goos:   "darwin",
			goarch: "arm64",
			want:   "MACOS",
		},
		{
			name:   "linux amd64",
			goos:   "linux",
			goarch: "amd64",
			want:   "LINUX",
		},
		{
			name:   "linux arm64",
			goos:   "linux",
			goarch: "arm64",
			want:   "LINUX",
		},
		{
			name:   "windows amd64",
			goos:   "windows",
			goarch: "amd64",
			want:   "WINDOWS",
		},
		{
			name:   "darwin other arch still macos",
			goos:   "darwin",
			goarch: "ppc64",
			want:   "MACOS",
		},
		{
			name:   "unknown os",
			goos:   "freebsd",
			goarch: "amd64",
			want:   "PLATFORM_UNSPECIFIED",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveCodeAssistPlatformFor(tc.goos, tc.goarch); got != tc.want {
				t.Fatalf("resolveCodeAssistPlatformFor(%q, %q) = %q, want %q", tc.goos, tc.goarch, got, tc.want)
			}
		})
	}
}
