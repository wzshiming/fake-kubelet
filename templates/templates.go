package templates

import (
	_ "embed"
)

var (
	//go:embed pod.status.tpl
	DefaultPodStatusTemplate string

	//go:embed node.tpl
	DefaultNodeTemplate string

	//go:embed node.heartbeat.tpl
	DefaultNodeHeartbeatTemplate string

	//go:embed node.initialization.tpl
	DefaultNodeInitializationTemplate string
)
