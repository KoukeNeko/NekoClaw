package core

import "testing"

func TestFriendlyErrorMessage(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{
			raw:  "no available account: provider=google-ai-studio reason=rate_limit retry_at=2026-04-10T12:00:00Z",
			want: "所有帳號目前都遇到速率限制，請稍後再試。",
		},
		{
			raw:  "You exceeded your current quota, please check your plan and billing details.",
			want: "所有帳號目前都遇到速率限制，請稍後再試。",
		},
		{
			raw:  "billing account not enabled",
			want: "帳號額度不足，請聯繫管理員。",
		},
		{
			raw:  "model_not_found: gemini-999",
			want: "找不到指定的模型。",
		},
		{
			raw:  "request timeout after 30s",
			want: "請求逾時，請稍後再試。",
		},
		{
			raw:  "no available account: provider=google-ai-studio",
			want: "目前沒有可用的帳號，請稍後再試。",
		},
		{
			raw:  "unexpected internal error",
			want: "unexpected internal error",
		},
	}
	for _, tc := range cases {
		t.Run(tc.raw[:min(len(tc.raw), 40)], func(t *testing.T) {
			got := FriendlyErrorMessage(tc.raw)
			if got != tc.want {
				t.Errorf("FriendlyErrorMessage(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
