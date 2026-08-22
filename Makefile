USER_ID  := $(shell id -u)
GROUP_ID := $(shell id -g)
GOPATH   := $(shell go env GOPATH)

.PHONY: run
run:
	go run .

.PHONY: build
build:
	go build -o maccleaner .

.PHONY: test
test:
	go test ./...

.PHONY: cover
cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

.PHONY: lint
lint:
	docker run -t --rm \
		-v "$(PWD)":/app \
		-w /app \
		-v /tmp/golintcache:/cache/go \
		-e GOCACHE=/cache/go \
		-e GOLANGCI_LINT_CACHE=/cache/go \
		-v "$(GOPATH)/pkg":/go/pkg \
		--user $(USER_ID):$(GROUP_ID) \
		golangci/golangci-lint:v2.10 sh -c "\
			golangci-lint run --output.text.path=stdout --output.code-climate.path=tmp/code-quality.json $(ADDITIONAL_FLAGS) $(DIRS) \
		"
