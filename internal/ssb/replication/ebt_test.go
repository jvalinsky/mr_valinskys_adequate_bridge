package replication

import (
	"encoding/json"
	"testing"
)

func TestNoteRoundTrip(t *testing.T) {
	// A valid ed25519 feed ref
	feed := "@6i7C39uHOD79Lz2I8N75M/E5xv8C0S6P2/9I8N75M/E=.ed25519"

	tests := []struct {
		name string
		note Note
	}{
		{"Replicate False", Note{Seq: 10, Replicate: false, Receive: true}},
		{"Replicate True, Receive True", Note{Seq: 10, Replicate: true, Receive: true}},
		{"Replicate True, Receive False", Note{Seq: 10, Replicate: true, Receive: false}},
		{"Seq 0", Note{Seq: 0, Replicate: true, Receive: true}},
		{"Seq -1", Note{Seq: -1, Replicate: true, Receive: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.note)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var nf NetworkFrontier
			wrapped := []byte(`{"` + feed + `": ` + string(b) + `}`)
			if err := json.Unmarshal(wrapped, &nf); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			got, ok := nf[feed]
			if !ok {
				t.Fatalf("Note for %s not found in unmarshaled result", feed)
			}
			
			if !tt.note.Replicate {
				if got.Replicate {
					t.Errorf("Replicate mismatch: got %v, want %v", got.Replicate, tt.note.Replicate)
				}
				return
			}

			if got.Replicate != tt.note.Replicate || got.Receive != tt.note.Receive {
				t.Errorf("Flags mismatch: got %+v, want %+v", got, tt.note)
			}
			
			expectedSeq := tt.note.Seq
			if expectedSeq == -1 {
				expectedSeq = 0 // -1 is normalized to 0 for Replicate=true
			}
			if got.Seq != expectedSeq {
				t.Errorf("Seq mismatch: got %d, want %d", got.Seq, expectedSeq)
			}
		})
	}
}
