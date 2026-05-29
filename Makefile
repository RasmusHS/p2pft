.PHONY: build build-cli build-relay test vet tidy run-relay clean
 
build: build-cli build-relay
 
build-cli:
	go build -o bin/p2pft ./cmd/p2pft
 
build-relay:
	go build -o bin/p2pft-relay ./cmd/p2pft-relay
 
test:
	go test ./...
 
vet:
	go vet ./...
 
tidy:
	go mod tidy
 
run-relay: build-relay
	RELAY_ADDR=:8080 ./bin/p2pft-relay
 
clean:
	rm -rf bin/
