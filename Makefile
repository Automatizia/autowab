BINARY=autowab
BUILD_DIR=./dist

.PHONY: build run clean

build:
	go build -tags sqlite_fts5 -o $(BUILD_DIR)/$(BINARY) ./cmd/autowab

run:
	AUTOWAB_TOKEN=dev123 go run -tags sqlite_fts5 ./cmd/autowab --port 3005

clean:
	rm -rf $(BUILD_DIR)
