package legacy

import (
	"bytes"
	"testing"
)

func TestCanonicalJSONOrdering(t *testing.T) {
	input := []byte(`{"z":1,"a":2,"m":3}`)
	
	output := CanonicalJSON(input)
	
	// Check order of keys in output
	zIdx := bytes.Index(output, []byte(`"z"`))
	aIdx := bytes.Index(output, []byte(`"a"`))
	mIdx := bytes.Index(output, []byte(`"m"`))
	
	if zIdx == -1 || aIdx == -1 || mIdx == -1 {
		t.Fatalf("Missing keys in output: %s", string(output))
	}
	
	if !(zIdx < aIdx && aIdx < mIdx) {
		t.Errorf("Expected order z, a, m; got relative indices z:%d, a:%d, m:%d", zIdx, aIdx, mIdx)
	}
}

func TestV8Binary(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []byte
	}{
		{"ASCII", "hello", []byte("hello")},
		{"BMP", "©", []byte{0xa9}}, // U+00A9
		{"Emoji", "👋", []byte{0x3d, 0x4b}}, // U+1F44B -> UTF16: D83D DC4B -> Low bytes: 3D 4B
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := V8Binary([]byte(tt.input))
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("V8Binary(%s) = %x, want %x", tt.input, got, tt.expected)
			}
		})
	}
}
