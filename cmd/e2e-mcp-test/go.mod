module github.com/jvalinsky/mr_valinskys_adequate_bridge/cmd/e2e-mcp-test

go 1.26.1

require github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb v0.0.0

require (
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
)

replace github.com/jvalinsky/mr_valinskys_adequate_bridge => /Users/jack/Software/mr_valinskys_adequate_bridge

replace github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb => /Users/jack/Software/mr_valinskys_adequate_bridge/internal/ssb
