package main

import (
	"fmt"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/bencode"
)

type TestStruct struct {
	A string
	B int
}

func main() {
	m := &TestStruct{A: "hello", B: 42}
	encoded, err := bencode.Encode(m)
	if err != nil {
		fmt.Printf("Bencode encode failed as expected: %v\n", err)
	} else {
		fmt.Printf("Bencode encode SUCCEEDED: %s\n", string(encoded))
	}
}
