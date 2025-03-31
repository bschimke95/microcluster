GOMIN=1.23.3
GOPATH ?= $(shell go env GOPATH)
DQLITE_PATH=$(GOPATH)/deps/dqlite
DQLITE_BRANCH=lts-1.17.x

.PHONY: default
default: update-schema

# Build dependencies
.PHONY: deps
deps:
	# dqlite (+raft)
	@if [ ! -e "$(DQLITE_PATH)" ]; then \
		echo "Retrieving dqlite from ${DQLITE_BRANCH} branch"; \
		git clone --depth=1 --branch "${DQLITE_BRANCH}" "https://github.com/canonical/dqlite" "$(DQLITE_PATH)"; \
	elif [ -e "$(DQLITE_PATH)/.git" ]; then \
		echo "Updating existing dqlite branch"; \
		cd "$(DQLITE_PATH)"; git pull; \
	fi

	cd "$(DQLITE_PATH)" && \
		autoreconf -i && \
		./configure --enable-build-raft && \
		make

# Testing targets.
.PHONY: check
check: check-static check-unit check-system

.PHONY: check-unit
check-unit:
	go test ./...

.PHONY: check-system
check-system:
	true

.PHONY: check-static
check-static:
ifeq ($(shell command -v golangci-lint 2> /dev/null),)
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v2.0.0
endif
	golangci-lint run --timeout 5m

# Update targets.
.PHONY: update-gomod
update-gomod:
	go get -t -v -u ./...

	# Static pins
	go get github.com/canonical/lxd@stable-5.21 # Stay on v2 dqlite and LXD LTS client

	go mod tidy -go=$(GOMIN)

	# Use the bundled toolchain that meets the minimum go version
	go get toolchain@none

# Update lxd-generate generated database helpers.
.PHONY: update-schema
update-schema:
	go generate ./cluster/...
	gofmt -s -w ./cluster/
	goimports -w ./cluster/
	@echo "Code generation completed"

