{{ with .status }}
{{ with .addresses }}
{{ . | YAML }}
{{ else }}
addresses:
- address: {{ NodeIP }}
  type: InternalIP
{{ end }}
{{ with .allocatable }}
{{ . | YAML }}
{{ else }}
allocatable:
  cpu: 1k
  memory: 1Ti
  pods: 1M
{{ end }}
{{ with .capacity }}
{{ . | YAML }}
{{ else }}
capacity:
  cpu: 1k
  memory: 1Ti
  pods: 1M
{{ end }}
{{ with .daemonEndpoints }}
{{ . | YAML }}
{{ else }}
daemonEndpoints:
  kubeletEndpoint:
    Port: 0
{{ end }}
nodeInfo:
{{ with .nodeInfo.architecture }}
  architecture: {{ . }}
{{ else }}
  architecture: amd64
{{ end }}
{{ with .nodeInfo.bootID }}
  bootID: {{ . }}
{{ end }}
{{ with .nodeInfo.containerRuntimeVersion }}
  containerRuntimeVersion: {{ . }}
{{ end }}
{{ with .nodeInfo.kernelVersion }}
  kernelVersion: {{ . }}
{{ end }}
{{ with .nodeInfo.kubeProxyVersion }}
  kubeProxyVersion: {{ . }}
{{ end }}
{{ with .nodeInfo.kubeletVersion }}
  kubeletVersion: {{ . }}
{{ else }}
  kubeletVersion: fake
{{ end }}
{{ with .nodeInfo.machineID }}
  machineID: {{ . }}
{{ end }}
{{ with .nodeInfo.operatingSystem }}
  operatingSystem: {{ . }}
{{ else }}
  operatingSystem: Linux
{{ end }}
{{ with .nodeInfo.osImage }}
  osImage: {{ . }}
{{ end }}
{{ with .nodeInfo.systemUUID }}
  systemUUID: {{ . }}
{{ end }}
phase: Running
{{ end }}