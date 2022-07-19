# fake-kubelet

[![Build](https://github.com/wzshiming/fake-kubelet/actions/workflows/go-cross-build.yml/badge.svg)](https://github.com/wzshiming/fake-kubelet/actions/workflows/go-cross-build.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/wzshiming/fake-kubelet)](https://goreportcard.com/report/github.com/wzshiming/fake-kubelet)
[![GitHub license](https://img.shields.io/github/license/wzshiming/fake-kubelet.svg)](https://github.com/wzshiming/fake-kubelet/blob/master/LICENSE)

This is a fake kubelet. that can simulate any number of nodes and maintain pods on those nodes.
It is useful for test control plane.

## What's the difference with [Kind](https://github.com/kubernetes-sigs/kind)

Kind is run Kubernetes in Docker that is a real cluster.

fake-kubelet is simulation an nodes of Kubernetes.

There is a [fake-k8s](https://github.com/wzshiming/fake-k8s) here, that can start a cluster using the fake-kubelet simulation nodes.

It can be used as an alternative to Kind in some scenarios where you don’t need to actually run the Pod.

## What's the difference with [Kubemark](https://github.com/kubernetes/kubernetes/tree/master/test/kubemark)

Kubemark is directly implemented with the code of kubelet, replacing the runtime part, 
except that it does not actually start the container, other behaviors are exactly the same as kubelet,
mainly used for Kubernetes own e2e test, simulating a large number of nodes and pods will occupy the same memory as the real scene.

fake-kubelet that only does the minimum work of maintaining nodes and pods, 
and is very suitable for simulating a large number of nodes and pods for pressure testing on the control plane.

## Usage

Deploying the fake-kubelet.

``` bash
kubectl apply -f https://raw.githubusercontent.com/wzshiming/fake-kubelet/master/deploy.yaml
```

`kubectl get node` You will find an fake nodes.

``` console
> kubectl get node -o wide
NAME         STATUS   ROLES   AGE   VERSION   INTERNAL-IP   EXTERNAL-IP   OS-IMAGE    KERNEL-VERSION   CONTAINER-RUNTIME
fake-0       Ready    agent   10s   fake      10.88.0.136   <none>        <unknown>   <unknown>        <unknown>
fake-1       Ready    agent   10s   fake      10.88.0.136   <none>        <unknown>   <unknown>        <unknown>
fake-2       Ready    agent   10s   fake      10.88.0.136   <none>        <unknown>   <unknown>        <unknown>
fake-3       Ready    agent   10s   fake      10.88.0.136   <none>        <unknown>   <unknown>        <unknown>
fake-4       Ready    agent   10s   fake      10.88.0.136   <none>        <unknown>   <unknown>        <unknown>
```

Deploying an application.

``` console
> kubectl apply -f - <<EOF
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
EOF
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

By adding the specified Annotation, the status of Pods can be modified independently.

Modify a container of pod as unready, and you will see the pod is `0/1` of ready.

``` console
> kubectl annotate pod fake-pod-78884479b7-52qcx --overwrite fake=custom
pod/fake-pod-78884479b7-52qcx annotated

> kubectl edit pod fake-pod-78884479b7-52qcx --subresource=status

> kubectl get pod fake-pod-78884479b7-52qcx -o wide
NAME                        READY   STATUS    RESTARTS   AGE   IP          NODE     NOMINATED NODE   READINESS GATES
fake-pod-78884479b7-52qcx   0/1     Running   0          6s    10.0.0.23   fake-4   <none>           <none>
```

Create a custom node, that content can be fully customized.
using Kubectl to modify node status must be added `--subresource=status`.

``` console
> kubectl apply -f - <<EOF
apiVersion: v1
kind: Node
metadata:
  annotations:
    node.alpha.kubernetes.io/ttl: "0"
  labels:
    app: fake-kubelet
    beta.kubernetes.io/arch: arm64
    beta.kubernetes.io/os: linux
    kubernetes.io/arch: arm64
    kubernetes.io/hostname: fake-arm-0
    kubernetes.io/os: linux
    kubernetes.io/role: agent
    node-role.kubernetes.io/agent: ""
    type: fake-kubelet # Matches to fake-kubelet's environment variable TAKE_OVER_LABELS_SELECTOR, this node will be taken over by fake-kubelet
  name: fake-arm-0
spec:
  taints: # Avoid scheduling actual running pods to fake Node
    - effect: NoSchedule
      key: fake-kubelet/provider
      value: fake
status:
  allocatable:
    cpu: 32
    memory: 256Gi
    pods: 110
  capacity:
    cpu: 32
    memory: 256Gi
    pods: 110
  nodeInfo:
    architecture: arm64
    bootID: ""
    containerRuntimeVersion: ""
    kernelVersion: ""
    kubeProxyVersion: fake
    kubeletVersion: fake
    machineID: ""
    operatingSystem: linux
    osImage: ""
    systemUUID: ""
  phase: Running
EOF
```

`kubectl get node` You will find the newly created `fake-arm-0` node, and that it is ready.

``` console
> kubectl get node -o wide
NAME         STATUS   ROLES   AGE   VERSION   INTERNAL-IP   EXTERNAL-IP   OS-IMAGE    KERNEL-VERSION   CONTAINER-RUNTIME
fake-0       Ready    agent   12s   fake      10.88.0.136   <none>        <unknown>   <unknown>        <unknown>
fake-1       Ready    agent   12s   fake      10.88.0.136   <none>        <unknown>   <unknown>        <unknown>
fake-2       Ready    agent   12s   fake      10.88.0.136   <none>        <unknown>   <unknown>        <unknown>
fake-3       Ready    agent   12s   fake      10.88.0.136   <none>        <unknown>   <unknown>        <unknown>
fake-4       Ready    agent   12s   fake      10.88.0.136   <none>        <unknown>   <unknown>        <unknown>
fake-arm-0   Ready    agent   2s    fake      10.88.0.136   <none>        <unknown>   <unknown>        <unknown>
```

## License

Licensed under the MIT License. See [LICENSE](https://github.com/wzshiming/fake-kubelet/blob/master/LICENSE) for the full license text.
