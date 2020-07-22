# fake-kubelet

[![Build Status](https://travis-ci.org/wzshiming/fake-kubelet.svg?branch=master)](https://travis-ci.org/wzshiming/fake-kubelet)
[![Go Report Card](https://goreportcard.com/badge/github.com/wzshiming/fake-kubelet)](https://goreportcard.com/report/github.com/wzshiming/fake-kubelet)
[![Docker Automated build](https://img.shields.io/docker/cloud/automated/wzshiming/fake-kubelet.svg)](https://hub.docker.com/r/wzshiming/fake-kubelet)
[![GitHub license](https://img.shields.io/github/license/wzshiming/fake-kubelet.svg)](https://github.com/wzshiming/fake-kubelet/blob/master/LICENSE)

This is a fake kubelet. The pod on this node will always be in the ready state, but no process will be started.

## Usage

Deploy fake kubelet.

``` yaml
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: fake-kubelet
  namespace: kube-system
  labels:
    app: fake-kubelet
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: fake-kubelet
  labels:
    app: fake-kubelet
rules:
  - apiGroups:
      - ""
    resources:
      - nodes
    verbs:
      - get
      - list
      - watch
  - apiGroups:
      - ""
    resources:
      - nodes/status
    verbs:
      - update
  - apiGroups:
      - ""
    resources:
      - pods/status
    verbs:
      - update
  - apiGroups:
      - ""
    resources:
      - pods
    verbs:
      - watch
      - list
      - delete
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: fake-kubelet
  labels:
    app: fake-kubelet
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: fake-kubelet
subjects:
  - kind: ServiceAccount
    name: fake-kubelet
    namespace: kube-system
---
apiVersion: v1
kind: Node
metadata:
  annotations:
    node.alpha.kubernetes.io/ttl: "0"
  labels:
    alpha.service-controller.kubernetes.io/exclude-balancer: "true"
    beta.kubernetes.io/os: linux
    kubernetes.io/hostname: fake
    kubernetes.io/os: linux
    kubernetes.io/role: agent
    type: virtual-kubelet
  name: fake
spec:
  taints:
    - effect: NoSchedule
      key: virtual-kubelet.io/provider
      value: fake
---
apiVersion: v1
kind: Pod
metadata:
  name: fake-kubelet
  namespace: kube-system
  labels:
    app: fake-kubelet
spec:
  containers:
    - name: fake-kubelet
      image: wzshiming/fake-kubelet
      imagePullPolicy: IfNotPresent
      env:
        - name: NODE_NAME
          value: fake
        - name: HOST_IP
          value: 10.0.0.1
        - name: NODE_IP
          valueFrom:
            fieldRef:
              fieldPath: status.podIP
  serviceAccount: fake-kubelet
  serviceAccountName: fake-kubelet
  restartPolicy: Always
---
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
