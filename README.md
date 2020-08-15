# fake-kubelet

[![Build Status](https://travis-ci.org/wzshiming/fake-kubelet.svg?branch=master)](https://travis-ci.org/wzshiming/fake-kubelet)
[![Go Report Card](https://goreportcard.com/badge/github.com/wzshiming/fake-kubelet)](https://goreportcard.com/report/github.com/wzshiming/fake-kubelet)
[![Docker Automated build](https://img.shields.io/docker/cloud/automated/wzshiming/fake-kubelet.svg)](https://hub.docker.com/r/wzshiming/fake-kubelet)
[![GitHub license](https://img.shields.io/github/license/wzshiming/fake-kubelet.svg)](https://github.com/wzshiming/fake-kubelet/blob/master/LICENSE)

This is a fake kubelet. The pod on this node will always be in the ready state, but no process will be started.

## Usage

Deploy fake kubelet.

``` bash
kubectl apply -f https://raw.githubusercontent.com/wzshiming/fake-kubelet/master/deploy.yaml
```

`kubectl get node` You will find a 'fake' node.

Deploy app.
``` yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: fake-pod
  namespace: default
spec:
  replicas: 10
  selector:
    matchLabels:
      app: fake-pod
  template:
    metadata:
      labels:
        app: fake-pod
    spec:
      containers:
        - name: fake-pod
          image: fake
      nodeName: fake # Direct scheduling to 'fake' node
```

`kubectl get pod` You will find that it has been started, although the image does not exist.

## License

Pouch is licensed under the MIT License. See [LICENSE](https://github.com/wzshiming/fake-kubelet/blob/master/LICENSE) for the full license text.
