package generate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ///////////////////////////////////////////////
// StripLeadingBanner
// ///////////////////////////////////////////////

func TestStripLeadingBanner_RemovesContiguousCommentAndBlankBlock(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "banner then value",
			input: "# Auto-generated\n" +
				"#\n" +
				"# section title\n" +
				"\n" +
				"version = 1\n",
			want: "version = 1\n",
		},
		{
			name:  "no banner leaves content untouched",
			input: "version = 1\nname = \"x\"\n",
			want:  "version = 1\nname = \"x\"\n",
		},
		{
			name:  "all comments strips everything",
			input: "# one\n# two\n",
			want:  "",
		},
		{
			name:  "indented comment is still a comment",
			input: "  # indented\nvalue = 1\n",
			want:  "value = 1\n",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name: "banner stops at first real line, preserves later comments",
			input: "# banner\n" +
				"version = 1\n" +
				"# inline section header\n" +
				"[capture]\n",
			want: "version = 1\n" +
				"# inline section header\n" +
				"[capture]\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(StripLeadingBanner([]byte(tt.input)))
			assert.Equal(t, tt.want, got)
		})
	}
}

// StripLeadingBanner must be idempotent: stripping an already-stripped
// file returns the same bytes. Guards against callers invoking it more
// than once across the write pipeline.
func TestStripLeadingBanner_Idempotent(t *testing.T) {
	input := "# banner\nversion = 1\n"
	once := StripLeadingBanner([]byte(input))
	twice := StripLeadingBanner(once)
	assert.Equal(t, string(once), string(twice), "a second strip changed the bytes")
	assert.True(t, strings.HasPrefix(string(once), "version"),
		"a second strip removed a real line: %q", string(once))
}
