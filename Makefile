OUT = ./build
GO = go

.PHONY: build

install:
	glide install

build:
	cd cmd/ba && \
	$(GO) build -o ../../$(OUT)/ba

test: build
