package refs

import (
	"testing"
)

func TestParseBlobRefRobust(t *testing.T) {
	tests := []string{
		"&Ufb/9IZaNBuIMKmG5FFDdCdtkAtHg4Zg8ikQOvxTXGk.sha256",
		"&xBTNDiBN6XT3N1PH4o12OOezaRu4saK6trJbt/7Xznc.sha256",
	}
	for _, test := range tests {
		ref, err := ParseBlobRef(test)
		if err != nil {
			t.Errorf("ParseBlobRef(%q) failed: %v", test, err)
		} else {
			t.Logf("ParseBlobRef(%q) success: %v", test, ref.Ref())
		}
	}
}
