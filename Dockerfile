FROM golang:alpine AS builder
WORKDIR /go/src/github.com/wzshiming/fake-kubelet
COPY . .
ENV CGO_ENABLED=0
RUN go install ./cmd/fake-kubelet

FROM alpine
RUN apk add curl
RUN curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" \
    && chmod +x kubectl \
    && cp kubectl /usr/bin
COPY --from=builder /go/bin/fake-kubelet /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/fake-kubelet", "--kubeconfig", "/root/.kube/config"]
