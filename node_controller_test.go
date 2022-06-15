package fake_kubelet

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wzshiming/fake-kubelet/templates"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNodeController(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node0",
				Labels: map[string]string{
					"fake": "true",
				},
			},
			Status: corev1.NodeStatus{
				Addresses: []corev1.NodeAddress{
					{
						Type:    corev1.NodeInternalIP,
						Address: "10.0.0.0",
					},
				},
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
			},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-not-match",
			},
			Status: corev1.NodeStatus{},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "other-node",
				Labels: map[string]string{
					"fake": "true",
				},
			},
			Status: corev1.NodeStatus{},
		},
	)

	defaultNodes := map[string]bool{
		"node1": true,
		"node2": true,
	}
	labelsSelector, _ := labels.Parse("fake=true")
	nodeSelectorFunc := func(node *corev1.Node) bool {
		return strings.HasPrefix(node.Name, "node")
	}
	nodes, err := NewNodeController(NodeControllerConfig{
		ClientSet:                  clientset,
		NodeIP:                     "10.0.0.1",
		NodeSelectorFunc:           nodeSelectorFunc,
		NodeLabelSelector:          labelsSelector.String(),
		NodeTemplate:               templates.DefaultNodeTemplate,
		NodeInitializationTemplate: templates.DefaultNodeInitializationTemplate,
		NodeHeartbeatTemplate:      templates.DefaultNodeHeartbeatTemplate,
		FuncMap:                    funcMap,
		NodeHeartbeatInterval:      1 * time.Second,
		NodeHeartbeatParallelism:   2,
		LockNodeParallelism:        2,
		Logger:                     testingLogger{t},
	})
	if err != nil {
		t.Fatal(fmt.Errorf("new nodes controller error: %v", err))
	}
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer func() {
		cancel()
		time.Sleep(time.Second)
	}()

	err = nodes.Start(ctx)
	if err != nil {
		t.Fatal(fmt.Errorf("failed to start nodes controller: %w", err))
	}

	for node := range defaultNodes {
		nodes.CreateNode(ctx, node)
	}

	time.Sleep(2 * time.Second)

	nodeSize := nodes.Size()
	if nodeSize != 3 {
		t.Fatal(fmt.Errorf("want 3 nodes, got %d", nodeSize))
	}

	node0, err := clientset.CoreV1().Nodes().Get(ctx, "node0", metav1.GetOptions{})
	if err != nil {
		t.Fatal(fmt.Errorf("failed to get node0: %w", err))
	}
	if node0.Status.Allocatable[corev1.ResourceCPU] != resource.MustParse("4") {
		t.Fatal(fmt.Errorf("node0 want 4 cpu, got %v", node0.Status.Allocatable[corev1.ResourceCPU]))
	}

	node1, err := clientset.CoreV1().Nodes().Get(ctx, "node1", metav1.GetOptions{})
	if err != nil {
		t.Fatal(fmt.Errorf("failed to get node1: %w", err))
	}
	if node1.Status.Allocatable[corev1.ResourceCPU] != resource.MustParse("1k") {
		t.Fatal(fmt.Errorf("node1 want 1k cpu, got %v", node1.Status.Allocatable[corev1.ResourceCPU]))
	}

	node3 := node0.DeepCopy()
	node3.Name = "node3"
	node3.Status.Allocatable[corev1.ResourceCPU] = resource.MustParse("8")
	_, err = clientset.CoreV1().Nodes().Create(ctx, node3, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(fmt.Errorf("failed to create node3: %w", err))
	}
	time.Sleep(2 * time.Second)

	nodeSize = nodes.Size()
	if nodeSize != 4 {
		t.Fatal(fmt.Errorf("want 4 nodes, got %d", nodeSize))
	}

	node3, err = clientset.CoreV1().Nodes().Get(ctx, "node3", metav1.GetOptions{})
	if err != nil {
		t.Fatal(fmt.Errorf("failed to get node3: %w", err))
	}
	if node3.Status.Allocatable[corev1.ResourceCPU] != resource.MustParse("8") {
		t.Fatal(fmt.Errorf("node3 want 8 cpu, got %v", node3.Status.Allocatable[corev1.ResourceCPU]))
	}

	list, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(fmt.Errorf("failed to list nodes: %w", err))
	}
	for _, node := range list.Items {
		if defaultNodes[node.Name] ||
			(nodeSelectorFunc(&node) && labelsSelector.Matches(labels.Set(node.Labels))) {
			if node.Status.Phase != corev1.NodeRunning {
				t.Fatal(fmt.Errorf("want node %s to be running, got %s", node.Name, node.Status.Phase))
			}
		} else {
			if node.Status.Phase == corev1.NodeRunning {
				t.Fatal(fmt.Errorf("want node %s to be not running, got %s", node.Name, node.Status.Phase))
			}
		}
	}
}
