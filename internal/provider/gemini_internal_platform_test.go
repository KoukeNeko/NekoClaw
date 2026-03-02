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
			want:   "DARWIN_AMD64",
		},
		{
			name:   "darwin arm64",
			goos:   "darwin",
			goarch: "arm64",
			want:   "DARWIN_ARM64",
		},
		{
			name:   "linux amd64",
			goos:   "linux",
			goarch: "amd64",
			want:   "LINUX_AMD64",
		},
		{
			name:   "linux arm64",
			goos:   "linux",
			goarch: "arm64",
			want:   "LINUX_ARM64",
		},
		{
			name:   "windows amd64",
			goos:   "windows",
			goarch: "amd64",
			want:   "WINDOWS_AMD64",
		},
		{
			name:   "unknown platform",
			goos:   "darwin",
			goarch: "ppc64",
			want:   "PLATFORM_UNSPECIFIED",
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
