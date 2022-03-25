FROM golang:alpine AS builder
RUN apk add -U git
WORKDIR /go/src/github.com/wzshiming/fake-kubelet
COPY . .
ENV CGO_ENABLED=0
RUN go install ./cmd/fake-kubelet

FROM alpine
COPY --from=builder /go/bin/fake-kubelet /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/fake-kubelet"]
