package fake_kubelet

import (
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

var (
	startTime = time.Now().Format(time.RFC3339)

	funcMap = template.FuncMap{
		"Now": func() string {
			return time.Now().Format(time.RFC3339)
		},
		"StartTime": func() string {
			return startTime
		},
		"YAML": func(s interface{}, indent ...int) (string, error) {
			d, err := yaml.Marshal(s)
			if err != nil {
				return "", err
			}

			data := string(d)
			if len(indent) == 1 && indent[0] > 0 {
				pad := strings.Repeat(" ", indent[0]*2)
				data = strings.Replace("\n"+data, "\n", "\n"+pad, -1)
			}
			return data, nil
		},
	}
)

// Controller is a fake kubelet implementation that can be used to test
type Controller struct {
	nodes *NodeController
	pods  *PodController
}

type Config struct {
	ClientSet                         kubernetes.Interface
	TakeOverAll                       bool
	TakeOverLabelsSelector            string
	PodCustomStatusAnnotationSelector string
	CIDR                              string
	NodeIP                            string
	Logger                            Logger
	PodStatusTemplate                 string
	NodeTemplate                      string
	NodeInitializationTemplate        string
	NodeHeartbeatTemplate             string
}

type Logger interface {
	Printf(format string, v ...interface{})
}

// NewController creates a new fake kubelet controller
func NewController(conf Config) (*Controller, error) {
	var nodeSelectorFunc func(node *corev1.Node) bool
	var nodeLabelSelector string
	if conf.TakeOverAll {
		nodeSelectorFunc = func(node *corev1.Node) bool {
			return true
		}
	} else if conf.TakeOverLabelsSelector != "" {
		selector, err := labels.Parse(conf.TakeOverLabelsSelector)
		if err != nil {
			return nil, err
		}
		nodeSelectorFunc = func(node *corev1.Node) bool {
			return selector.Matches(labels.Set(node.Labels))
		}
		nodeLabelSelector = selector.String()
	}

	var lockPodsOnNodeFunc func(ctx context.Context, nodeName string) error

	nodes, err := NewNodeController(NodeControllerConfig{
		ClientSet:         conf.ClientSet,
		NodeIP:            conf.NodeIP,
		NodeSelectorFunc:  nodeSelectorFunc,
		NodeLabelSelector: nodeLabelSelector,
		LockPodsOnNodeFunc: func(nodeName string) error {
			return lockPodsOnNodeFunc(context.Background(), nodeName)
		},
		NodeTemplate:               conf.NodeTemplate,
		NodeInitializationTemplate: conf.NodeInitializationTemplate,
		NodeHeartbeatTemplate:      conf.NodeHeartbeatTemplate,
		NodeHeartbeatInterval:      30 * time.Second,
		NodeHeartbeatParallelism:   16,
		LockNodeParallelism:        16,
		Logger:                     conf.Logger,
		FuncMap:                    funcMap,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create nodes controller: %v", err)
	}

	pods, err := NewPodController(PodControllerConfig{
		ClientSet:                         conf.ClientSet,
		NodeIP:                            conf.NodeIP,
		CIDR:                              conf.CIDR,
		PodCustomStatusAnnotationSelector: conf.PodCustomStatusAnnotationSelector,
		PodStatusTemplate:                 conf.PodStatusTemplate,
		LockPodParallelism:                16,
		DeletePodParallelism:              16,
		NodeHasFunc:                       nodes.Has, // just handle pods that are on nodes we have
		Logger:                            conf.Logger,
		FuncMap:                           funcMap,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create pods controller: %v", err)
	}

	lockPodsOnNodeFunc = pods.LockPodsOnNode

	n := &Controller{
		pods:  pods,
		nodes: nodes,
	}

	return n, nil
}

func (c *Controller) Start(ctx context.Context) error {
	err := c.pods.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start pods controller: %v", err)
	}
	err = c.nodes.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start nodes controller: %v", err)
	}
	return nil
}

func (c *Controller) CreateNode(ctx context.Context, nodeName string) error {
	return c.nodes.CreateNode(ctx, nodeName)
}
