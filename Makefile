.PHONY: build

NAME := $(shell basename $${PWD})

test-v:
	go test ./... -v  -timeout=60000ms

test:
	go test ./...
	go test ./... -short -race
	go vet

docker:
	docker build -t $(NAME) . -f Dockerfile

build: docker init

init:
	cat init.sh | sed -e 's/%NAME%/$(NAME)/' > $(NAME).sh
	chmod 755 $(NAME).sh

