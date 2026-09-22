.PHONY: build test vet clean install docker-build docker-run

BINARY := codergag
CMD := ./cmd/codergag

.PHONY: build
build:
	go build -o $(BINARY) $(CMD)

.PHONY: build-static
build-static:
	CGO_ENABLED=1 CC=musl-gcc go build -trimpath -ldflags '-s -w -linkmode external -extldflags "-static"' -o $(BINARY) $(CMD)

.PHONY: test
test:
	go test -race ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: check
check: vet test

.PHONY: clean
clean:
	rm -f $(BINARY)
	rm -rf ./codergag-test

.PHONY: install
install:
	CGO_ENABLED=1 go build -o $(BINARY) $(CMD)
	install -m 0755 $(BINARY) $(HOME)/.local/bin/$(BINARY)

.PHONY: docker-build
docker-build:
	docker build -t codergag:latest .

.PHONY: docker-run
docker-run: docker-build
	docker run -p 8080:8080 codergag:latest
