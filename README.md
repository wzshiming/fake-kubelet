# fake-kubelet

[![Build Status](https://travis-ci.org/wzshiming/fake-kubelet.svg?branch=master)](https://travis-ci.org/wzshiming/fake-kubelet)
[![Go Report Card](https://goreportcard.com/badge/github.com/wzshiming/fake-kubelet)](https://goreportcard.com/report/github.com/wzshiming/fake-kubelet)
[![Docker Automated build](https://img.shields.io/docker/cloud/automated/wzshiming/fake-kubelet.svg)](https://hub.docker.com/r/wzshiming/fake-kubelet)
[![GitHub license](https://img.shields.io/github/license/wzshiming/fake-kubelet.svg)](https://github.com/wzshiming/fake-kubelet/blob/master/LICENSE)

This is a fake kubelet. The pod on this node will always be in the ready state, but no process will be started.

## What's the difference with [Kind](https://github.com/kubernetes-sigs/kind)

Kind is run Kubernetes in Docker that is a real cluster.

fake-kubelet is simulation a node of Kubernetes.

There is a [fake-k8s](https://github.com/wzshiming/fake-k8s) here, that can start a cluster using the fake-kubelet simulation node using docker-compose.

It can be used as an alternative to Kind in some scenarios where you don’t need to actually run the Pod.

## What's the difference with [Kubemark](https://github.com/kubernetes/kubernetes/tree/master/test/kubemark)

Kubemark is directly implemented with the code of kubelet, replacing the runtime part, 
except that it does not actually start the container, other behaviors are exactly the same as kubelet,
mainly used for Kubernetes own e2e test, simulating a large number of nodes and pods will occupy the same memory as the real scene.

fake-kubelet that only does the minimum work of maintaining nodes and pods, 
and is very suitable for simulating a large number of nodes and pods for pressure testing on the control plane.

## Usage

Deploy fake kubelet.

``` bash
kubectl apply -f https://raw.githubusercontent.com/wzshiming/fake-kubelet/master/deploy.yaml
```

`kubectl get node` You will find a fake node.


``` console
> kubectl get node -o wide
NAME         STATUS   ROLES   AGE   VERSION   INTERNAL-IP   EXTERNAL-IP   OS-IMAGE    KERNEL-VERSION   CONTAINER-RUNTIME
fake-0       Ready    agent   10s   fake      10.88.0.136   <none>        <unknown>   <unknown>        <unknown>
fake-1       Ready    agent   10s   fake      10.88.0.136   <none>        <unknown>   <unknown>        <unknown>
fake-2       Ready    agent   10s   fake      10.88.0.136   <none>        <unknown>   <unknown>        <unknown>
fake-3       Ready    agent   10s   fake      10.88.0.136   <none>        <unknown>   <unknown>        <unknown>
fake-4       Ready    agent   10s   fake      10.88.0.136   <none>        <unknown>   <unknown>        <unknown>
```

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
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: type
                    operator: In
                    values:
                      - fake-kubelet
      tolerations: # A taints was added to an automatically created Node. You can remove taints of Node or add this tolerations
        - key: "fake-kubelet/provider"
          operator: "Exists"
          effect: "NoSchedule"
      # nodeName: fake-0 # Or direct scheduling to a fake node
      containers:
        - name: fake-pod
          image: fake
```

`kubectl get pod` You will find that it has been started, although the image does not exist.

``` console
> kubectl get pod -o wide
NAME                        READY   STATUS    RESTARTS   AGE   IP          NODE     NOMINATED NODE   READINESS GATES
fake-pod-78884479b7-52qcx   1/1     Running   0          6s    10.0.0.23   fake-4   <none>           <none>
fake-pod-78884479b7-bd6nk   1/1     Running   0          6s    10.0.0.13   fake-2   <none>           <none>
fake-pod-78884479b7-dqjtn   1/1     Running   0          6s    10.0.0.15   fake-2   <none>           <none>
fake-pod-78884479b7-h2fv6   1/1     Running   0          6s    10.0.0.31   fake-0   <none>           <none>
fake-pod-78884479b7-hc9kd   1/1     Running   0          6s    10.0.0.29   fake-4   <none>           <none>
fake-pod-78884479b7-m4rb8   1/1     Running   0          6s    10.0.0.19   fake-1   <none>           <none>
fake-pod-78884479b7-p9zmn   1/1     Running   0          6s    10.0.0.27   fake-0   <none>           <none>
fake-pod-78884479b7-pmgmf   1/1     Running   0          6s    10.0.0.21   fake-0   <none>           <none>
fake-pod-78884479b7-rzbs2   1/1     Running   0          6s    10.0.0.17   fake-0   <none>           <none>
fake-pod-78884479b7-scsjb   1/1     Running   0          6s    10.0.0.25   fake-1   <none>           <none>
```

## License

Pouch is licensed under the MIT License. See [LICENSE](https://github.com/wzshiming/fake-kubelet/blob/master/LICENSE) for the full license text.
