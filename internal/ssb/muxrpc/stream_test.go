package muxrpc

import (
	"fmt"
	"testing"
)

func TestByteSink_CloseWithError_IsCounter(t *testing.T) {
	bs := &ByteSink{
		req:       42,
		isCounter: true,
		closed:    false,
	}

	bs.CloseWithError(fmt.Errorf("test error"))

	if bs.isCounter != true {
		t.Errorf("isCounter should be preserved")
	}

	if !bs.closed {
		t.Errorf("should be marked as closed")
	}
}
