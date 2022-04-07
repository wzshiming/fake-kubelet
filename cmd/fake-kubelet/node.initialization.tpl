{{ if not .status.addresses }}
addresses:
- address: {{ NodeIP }}
  type: InternalIP
{{ end }}
{{ if not .status.allocatable }}
allocatable:
  cpu: 1k
  memory: 1Ti
  pods: 1M
{{ end }}
{{ if not .status.capacity }}
capacity:
  cpu: 1k
  memory: 1Ti
  pods: 1M
{{ end }}
{{ if not .status.daemonEndpoints }}
daemonEndpoints:
  kubeletEndpoint:
    Port: 0
{{ end }}
{{ if not .status.nodeInfo }}
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
{{ end }}
phase: Running