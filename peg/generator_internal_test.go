package peg

import (
	"bytes"
	"testing"
)

func TestLabelDecorators(t *testing.T) {
	for _, label := range []string{"", "   ", "\t"} {
		if decorators := labelDecorators(label); len(decorators) != 0 {
			t.Fatalf("labelDecorators(%q) returned %d decorators, want none", label, len(decorators))
		}
	}

	if decorators := labelDecorators(" smoke "); len(decorators) != 1 {
		t.Fatalf("labelDecorators() returned %d decorators, want one", len(decorators))
	}
}

func TestWriteAssertionOutput(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want string
	}{
		{
			name: "output ending in newline",
			out:  "hello\n",
			want: "Command output:\nhello\n",
		},
		{
			name: "output without newline",
			out:  "Linux",
			want: "Command output:\nLinux\n",
		},
		{
			name: "empty output",
			want: "Command output:\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bytes.Buffer
			writeAssertionOutput(&got, tt.out)
			if got.String() != tt.want {
				t.Fatalf("output = %q, want %q", got.String(), tt.want)
			}
		})
	}
}
