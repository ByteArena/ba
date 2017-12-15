OUT = ./bin
GO = go
DATE = $(shell date '+%Y-%m-%d_%H:%M:%S')

.PHONY: build

init:
	glide install

build:
	cd cmd/ba && \
	$(GO) build -o ../../$(OUT)/ba \
		-ldflags="-X github.com/bytearena/ba/vendor/github.com/bytearena/core/common/utils.version=dev-$(DATE)"

install: build
	cp $(OUT)/ba /usr/local/bin/ba

test: build
