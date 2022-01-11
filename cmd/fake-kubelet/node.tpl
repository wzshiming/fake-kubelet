apiVersion: v1
kind: Node
metadata:
  annotations:
    node.alpha.kubernetes.io/ttl: "0"
  labels:
    app: fake-kubelet
    beta.kubernetes.io/arch: amd64
    beta.kubernetes.io/os: linux
    kubernetes.io/arch: amd64
    kubernetes.io/hostname: {{ .metadata.name }}
    kubernetes.io/os: linux
    kubernetes.io/role: agent
    node-role.kubernetes.io/agent: ""
    type: fake-kubelet
  name: {{ .metadata.name }}
spec:
  taints:
    - effect: NoSchedule
      key: fake-kubelet/provider
      value: fake