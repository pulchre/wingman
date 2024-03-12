.PHONY: all clean test install-protobuf

WINGMAN_TESTS ?= ...
go_files = $(shell find ./ \! -path './mock/*' \! -name '*_test.go' -type f -name '*.go' -print | sed 's/^.\///')
processor_proto = grpc/processor.pb.go

native_testcmdbin = processor/native/testcmdbin

all:

clean:
	rm -rf $(native_testcmdbin)

test: $(native_testcmdbin) $(processor_proto)
	go test -count 1 -cover -timeout 10s github.com/pulchre/wingman/$(WINGMAN_TESTS)

$(processor_proto): grpc/processor.proto grpc/grpc.go
	go generate github.com/pulchre/wingman/grpc

$(native_testcmdbin): $(go_files) $(processor_proto)
	go build -o $(native_testcmdbin) github.com/pulchre/wingman/processor/native/testcmd

install-protobuf-plugin:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
