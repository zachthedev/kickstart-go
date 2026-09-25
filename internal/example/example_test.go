package example

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGreet(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "with name", input: "Go", want: "hello, Go"},
		{name: "empty name", input: "", want: "hello, world"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Greet(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
