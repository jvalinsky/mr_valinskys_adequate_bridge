.PHONY: test-unit test-smoke test-e2e-room test-e2e-full test-linux test-ebpf test-all clean

test-unit:
	GOFLAGS=-mod=mod go test ./...

test-smoke:
	GOFLAGS=-mod=mod go test ./internal/smoke -run TestBridgeSmoke

test-e2e-room:
	./scripts/e2e_tildefriends.sh

test-e2e-full:
	./scripts/e2e_full_up.sh

test-linux:
	docker compose -f infra/linux-test/docker-compose.yml up go-test

test-ebpf:
	docker compose -f infra/linux-test/docker-compose.yml --profile ebpf up ebpf-smoke

test-all: test-unit test-smoke test-e2e-room

clean:
	rm -f bridge-cli atproto-seed e2e-seed ssb-client ssb-client-test
	rm -f *.log *.sqlite *.pid
