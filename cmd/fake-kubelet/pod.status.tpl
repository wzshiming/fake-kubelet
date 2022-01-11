{{ $startTime := .metadata.creationTimestamp }}
conditions:
- lastTransitionTime: {{ $startTime }}
  status: "True"
  type: Initialized
- lastTransitionTime: {{ $startTime }}
  status: "True"
  type: Ready
- lastTransitionTime: {{ $startTime }}
  status: "True"
  type: ContainersReady
- lastTransitionTime: {{ $startTime }}
  status: "True"
  type: PodScheduled
{{ range .spec.readinessGates }}
- lastTransitionTime: {{ $startTime }}
  status: "True"
  type: {{ .conditionType }}
{{ end }}
{{ if .spec.containers }}
containerStatuses:
{{ range .spec.containers }}
- image: {{ .image }}
  name: {{ .name }}
  ready: true
  restartCount: 0
  state:
    running:
      startedAt: {{ $startTime }}
{{ end }}
{{ end }}
{{ if .spec.initContainers }}
initContainerStatuses:
{{ range .spec.initContainers }}
- image: {{ .image }}
  name: {{ .name }}
  ready: true
  restartCount: 0
  state:
    terminated:
      exitCode: 0
      finishedAt: {{ $startTime }}
      reason: Completed
      startedAt: {{ $startTime }}
{{ end }}
{{ end }}
phase: Running
startTime: {{ $startTime }}
hostIP: {{ if .status.hostIP }} {{ .status.hostIP }} {{ else }} {{ NodeIP }} {{ end }}
podIP: {{ if .status.podIP }} {{ .status.podIP }} {{ else }} {{ PodIP }} {{ end }}