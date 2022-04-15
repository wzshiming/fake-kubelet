{{ with .status }}
addresses:
{{ with .addresses }}
{{ . | YAML }}
{{ else }}
- address: {{ NodeIP }}
  type: InternalIP
{{ end }}
allocatable:
{{ with .allocatable }}
{{ . | YAML }}
{{ else }}
  cpu: 1k
  memory: 1Ti
  pods: 1M
{{ end }}
capacity:
{{ with .capacity }}
{{ . | YAML }}
{{ else }}
  cpu: 1k
  memory: 1Ti
  pods: 1M
{{ end }}
daemonEndpoints:
{{ with .daemonEndpoints }}
{{ . | YAML }}
{{ else }}
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