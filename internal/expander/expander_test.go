//go:build darwin

package expander

import (
	"strings"
	"testing"
	"unicode/utf16"

	"sokki/internal/model"
)

func TestChunkKeepsCharactersWhole(t *testing.T) {
	s := strings.Repeat("a", 150) + strings.Repeat("😀", 60) // 150 + 120 UTF-16 units
	chunks := chunk(s, 200)
	if strings.Join(chunks, "") != s {
		t.Fatal("chunks do not add up")
	}
	for _, c := range chunks {
		if n := len(utf16.Encode([]rune(c))); n > 200 {
			t.Fatalf("chunk of %d units", n)
		}
	}
	if len(chunk("", 200)) != 0 || len(chunk("abc", 200)) != 1 {
		t.Fatal("small inputs")
	}
}

func TestPlaceholderFrames(t *testing.T) {
	if placeholderFrame(model.PlaceholderStatic, 3) != placeholderWord {
		t.Fatal("static")
	}
	if placeholderFrame(model.PlaceholderBraille, 10) != brailleFrames[0] || placeholderFrame(model.PlaceholderBlockSweep, 9) != blockFrames[1] {
		t.Fatal("frames wrap")
	}
}
