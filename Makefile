BINARY ?= kube-applier-gcp
IMAGE  ?= kube-applier-gcp
TAG    ?= latest

.PHONY: build test vet image clean

build:
	go build -o $(BINARY) .

test:
	go test ./... -count=1

vet:
	go vet ./...

image:
	docker build -t $(IMAGE):$(TAG) .

clean:
	rm -f $(BINARY)
