package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientKeyFromRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		want       string
	}{
		{
			name:       "prefers cloudflare connecting ip over forwarded chain",
			remoteAddr: "10.42.0.15:8085",
			headers: map[string]string{
				"CF-Connecting-IP": "198.51.100.24",
				"X-Forwarded-For":  "203.0.113.7, 10.42.0.15",
			},
			want: "198.51.100.24",
		},
		{
			name:       "parses standardized forwarded ipv6 address",
			remoteAddr: "10.42.0.15:8085",
			headers: map[string]string{
				"Forwarded": `for="[2001:db8:cafe::17]:4711";proto=https;host="nekoclaw.koukeneko.cafe"`,
			},
			want: "2001:db8:cafe::17",
		},
		{
			name:       "skips unknown forwarded identifiers and falls back",
			remoteAddr: "198.51.100.30:43122",
			headers: map[string]string{
				"Forwarded": `for=unknown;proto=https`,
				"X-Real-IP": "203.0.113.88",
			},
			want: "203.0.113.88",
		},
		{
			name:       "falls back to remote addr",
			remoteAddr: "198.51.100.25:43122",
			want:       "198.51.100.25",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			if got := clientKeyFromRequest(req); got != tt.want {
				t.Fatalf("clientKeyFromRequest() = %q, want %q", got, tt.want)
			}
		})
	}
}
