addresses:
- address: {{ NodeIP }}
  type: InternalIP
allocatable:
  cpu: 1k
  memory: 1Ti
  pods: 1M
capacity:
  cpu: 1k
  memory: 1Ti
  pods: 1M
daemonEndpoints:
  kubeletEndpoint:
    Port: 0
nodeInfo:
  architecture: amd64
  bootID: ""
  containerRuntimeVersion: ""
  kernelVersion: ""
  kubeProxyVersion: ""
  kubeletVersion: fake
  machineID: ""
  operatingSystem: Linux
  osImage: ""
  systemUUID: ""
phase: Running