 #set -x
 #set -e

 echo -----------
 echo WARNING!!!!
 echo -----------
 echo This script may or may not work. It was reconstructed from my shell history.
 echo If it does not work it can certianly be made to work.
 sleep 1

 go install google.golang.org/protobuf/cmd/protoc-gen-go
 go install google.golang.org/grpc/cmd/protoc-gen-go-grpc
 go get -u github.com/golang/protobuf/{proto,protoc-gen-go}
 go get -u google.golang.org/grpc
 export PATH="$PATH:$(go env GOPATH)/bin"
 protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative proto/fakekubelet.proto
 #protoc --go_out=plugins=grpc:. proto/*.proto
