package types

import "testing"

func TestNormalizeSessionType(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "selenium", input: "selenium", want: "selenium"},
		{name: "playwright", input: "playwright", want: "playwright"},
		{name: "mcp", input: "mcp", want: "mcp"},
		{name: "devtools", input: "devtools", want: "devtools"},
		{name: "empty", input: "", want: SessionTypeUnknown},
		{name: "unknown", input: "cdp", want: SessionTypeUnknown},
		{name: "wrong case", input: "Selenium", want: SessionTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeSessionType(tt.input); got != tt.want {
				t.Fatalf("NormalizeSessionType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
