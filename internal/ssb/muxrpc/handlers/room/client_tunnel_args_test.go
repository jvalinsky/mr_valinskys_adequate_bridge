package room

import (
	"bytes"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

func TestParseClientTunnelConnectArgsParsesTarget(t *testing.T) {
	target := refs.MustNewFeedRef(bytes.Repeat([]byte{0x44}, 32), refs.RefAlgoFeedSSB1)

	args, err := parseClientTunnelConnectArgs([]byte(`[{"target":"` + target.String() + `"}]`))
	if err != nil {
		t.Fatalf("parse client tunnel args: %v", err)
	}
	if !args.Target.Equal(*target) {
		t.Fatalf("expected target %s, got %s", target.String(), args.Target.String())
	}
}
