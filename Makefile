.PHONY: proto
proto:
	go install tool
	PATH="$$(go env GOPATH)/bin:$$PATH" protoc \
		--proto_path=proto \
		--go_out=. \
		--go_opt=module=poltergeist \
		proto/aircount/v1/*.proto
	cp proto/aircount/v1/*.proto internal/pb/aircount/v1/

