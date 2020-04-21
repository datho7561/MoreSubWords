

default: build

build:
	cd server && go build

run: build
	./server/MoreSubWords