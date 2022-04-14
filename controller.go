package fake_kubelet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"text/template"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

const mergeLabel = "fake/status"

// Controller is a fake kubelet implementation that can be used to test
type Controller struct {
	cidrIPNet                  *net.IPNet
	nodeIP                     string
	takeOverAll                bool // if true, take over all nodes
	nodes                      []string
	nodePoolMut                sync.RWMutex
	nodePool                   map[string]struct{}
	clientSet                  *kubernetes.Clientset
	ipPool                     *ipPool
	statusTemplate             string
	nodeTemplate               string
	nodeHeartbeatTemplate      string
	nodeInitializationTemplate string
	funcMap                    template.FuncMap
	logger                     Logger
}

type Logger interface {
	Printf(format string, v ...interface{})
}

// NewController creates a new fake kubelet controller
func NewController(clientSet *kubernetes.Clientset,
	nodes []string, takeOverAll bool,
	cidr string, nodeIP string,
	logger Logger,
	statusTemplate, nodeTemplate, nodeHeartbeatTemplate, nodeInitializationTemplate string) (*Controller, error) {
	startTime := time.Now().Format(time.RFC3339)
	cidrIPNet, err := parseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	n := &Controller{
		clientSet:                  clientSet,
		nodes:                      nodes,
		takeOverAll:                takeOverAll,
		cidrIPNet:                  cidrIPNet,
		nodeIP:                     nodeIP,
		nodePool:                   map[string]struct{}{},
		ipPool:                     newIPPool(cidrIPNet),
		logger:                     logger,
		statusTemplate:             statusTemplate,
		nodeTemplate:               nodeTemplate,
		nodeHeartbeatTemplate:      nodeHeartbeatTemplate,
		nodeInitializationTemplate: nodeInitializationTemplate,
	}
	n.funcMap = template.FuncMap{
		"Now": func() string {
			return time.Now().Format(time.RFC3339)
		},
		"StartTime": func() string {
			return startTime
		},
		"NodeIP": func() string {
			return nodeIP
		},
		"PodIP": func() string {
			return n.ipPool.Get()
		},
	}
	return n, nil
}

var (
	removeFinalizers = []byte(`{"metadata":{"finalizers":null}}`)
	deleteOpt        = *metav1.NewDeleteOptions(0)
	podFieldSelector = fields.OneTermNotEqualSelector("spec.nodeName", "").String()
)

func (c *Controller) deletePod(ctx context.Context, pod *corev1.Pod) error {
	if len(pod.Finalizers) != 0 {
		_, err := c.clientSet.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.MergePatchType, removeFinalizers, metav1.PatchOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
	}

	err := c.clientSet.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, deleteOpt)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func (c *Controller) lockPod(ctx context.Context, pod *corev1.Pod) error {
	if pod.DeletionTimestamp != nil {
		if c.cidrIPNet.Contains(net.ParseIP(pod.Status.PodIP)) {
			c.ipPool.Put(pod.Status.PodIP)
		}
		err := c.deletePod(ctx, pod)
		if err != nil {
			return err
		}
		if c.logger != nil {
			c.logger.Printf("Delete pod %s.%s on %s", pod.Name, pod.Namespace, pod.Spec.NodeName)
		}
		return nil
	}

	ok, err := c.configurePod(pod)
	if err != nil {
		return err
	}
	if !ok {
		if c.logger != nil {
			c.logger.Printf("Skip pod %s.%s on %s", pod.Name, pod.Namespace, pod.Spec.NodeName)
		}
		return nil
	}
	pod.ResourceVersion = "0"
	_, err = c.clientSet.CoreV1().Pods(pod.Namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if c.logger != nil {
		c.logger.Printf("Lock pod %s.%s on %s", pod.Name, pod.Namespace, pod.Spec.NodeName)
	}
	return nil
}

// LockPodStatus locks the pods status
func (c *Controller) LockPodStatus(ctx context.Context) error {
	tasks := newParallelTasks(16)
	lockCh := make(chan *corev1.Pod)
	go func() {
		for {
			select {
			case pod, ok := <-lockCh:
				if !ok {
					return
				}

				localPod := pod
				tasks.Add(func() {
					err := c.lockPod(ctx, localPod)
					if err != nil {
						if c.logger != nil {
							c.logger.Printf("Failed to lock pod %s.%s on %s: %s", localPod.Name, localPod.Namespace, localPod.Spec.NodeName, err)
						}
					}
				})
			case <-ctx.Done():
				return
			}
		}
	}()

	opt := metav1.ListOptions{
		FieldSelector: podFieldSelector,
	}

	watcher, err := c.clientSet.CoreV1().Pods(corev1.NamespaceAll).Watch(ctx, opt)
	if err != nil {
		return err
	}

	go func() {
		rc := watcher.ResultChan()
	loop:
		for {
			select {
			case event, ok := <-rc:
				if !ok {
					for {
						watcher, err := c.clientSet.CoreV1().Pods(corev1.NamespaceAll).Watch(ctx, opt)
						if err == nil {
							rc = watcher.ResultChan()
							continue loop
						}

						if c.logger != nil {
							c.logger.Printf("Failed to watch pods: %s", err)
						}
						select {
						case <-ctx.Done():
							break loop
						case <-time.After(time.Second * 5):
						}
					}
				}
				switch event.Type {
				case watch.Added:
					pod := event.Object.(*corev1.Pod)
					if c.hasNode(pod.Spec.NodeName) {
						lockCh <- pod
					}
				case watch.Modified:
					pod := event.Object.(*corev1.Pod)
					if pod.DeletionTimestamp != nil {
						if c.hasNode(pod.Spec.NodeName) {
							lockCh <- pod
						}
					}
				}
			case <-ctx.Done():
				watcher.Stop()
				break loop
			}
		}
		if c.logger != nil {
			c.logger.Printf("Stop locking pod status")
		}
	}()

	return nil
}

func (c *Controller) lockNode(ctx context.Context, node *corev1.Node) error {
	patch, err := c.configureNode(node)
	if err != nil {
		return err
	}
	_, err = c.clientSet.CoreV1().Nodes().PatchStatus(ctx, node.Name, patch)
	return err
}

func (c *Controller) heartbeatNode(ctx context.Context, nodeName string) error {
	var node corev1.Node
	node.Name = nodeName
	patch, err := c.configureHeartbeatNode(&node)
	if err != nil {
		return err
	}
	_, err = c.clientSet.CoreV1().Nodes().PatchStatus(ctx, node.Name, patch)
	return err
}

func (c *Controller) allHeartbeatNode(ctx context.Context) {
	tasks := newParallelTasks(16)
	c.nodePoolMut.RLock()
	defer c.nodePoolMut.RUnlock()
	for node := range c.nodePool {
		if node == "" {
			continue
		}
		localNode := node
		tasks.Add(func() {
			err := c.heartbeatNode(ctx, localNode)
			if err != nil {
				if c.logger != nil {
					c.logger.Printf("Failed to heartbeat node %s: %s", localNode, err)
				}
			}
		})
	}
	tasks.Wait()
}

func (c *Controller) hasNode(node string) bool {
	c.nodePoolMut.RLock()
	_, ok := c.nodePool[node]
	c.nodePoolMut.RUnlock()
	return ok
}

func (c *Controller) deleteNode(node string) {
	c.nodePoolMut.Lock()
	delete(c.nodePool, node)
	c.nodePoolMut.Unlock()
	return
}

func (c *Controller) Start(ctx context.Context) error {
	err := c.LockPodStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed lock pod status: %w", err)
	}
	err = c.LockNodeStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed lock node status: %w", err)
	}
	return c.LockAllPodStatusOnce(ctx)
}

// LockAllPodStatusOnce locks existing or missing pods status
func (c *Controller) LockAllPodStatusOnce(ctx context.Context) error {
	var limit int64 = 128
	continueStr := ""
	for {
		list, err := c.clientSet.CoreV1().Pods(corev1.NamespaceAll).List(ctx, metav1.ListOptions{
			FieldSelector: podFieldSelector,
			Limit:         limit,
			Continue:      continueStr,
		})
		if err != nil {
			return err
		}
		for _, pod := range list.Items {
			if !c.hasNode(pod.Spec.NodeName) {
				continue
			}
			err := c.lockPod(ctx, &pod)
			if err != nil {
				if c.logger != nil {
					c.logger.Printf("Failed to lock pod %s.%s on %s: %s", pod.Name, pod.Namespace, pod.Spec.NodeName, err)
				}
			}
		}
		if list.Continue == "" {
			break
		}
		continueStr = list.Continue
	}
	return nil
}

// LockNodeStatus locks the nodes status
func (c *Controller) LockNodeStatus(ctx context.Context) error {
	heartbeatInterval := 30 * time.Second
	th := time.NewTimer(heartbeatInterval)
	go func() {
		for {
			select {
			case <-th.C:
				th.Reset(heartbeatInterval)
				c.allHeartbeatNode(ctx)
			case <-ctx.Done():
				if c.logger != nil {
					c.logger.Printf("Stop locking nodes %s status", c.nodes)
				}
				return
			}
		}
	}()

	ch := make(chan string)
	go c.lockNodes(ctx, ch)
	for _, node := range c.nodes {
		ch <- node
	}

	if !c.takeOverAll {
		close(ch)
		return nil
	}

	// Take over all nodes in the cluster
	opt := metav1.ListOptions{}
	watcher, err := c.clientSet.CoreV1().Nodes().Watch(ctx, opt)
	if err != nil {
		return err
	}
	go func() {
		rc := watcher.ResultChan()
	loop:
		for {
			select {
			case event, ok := <-rc:
				if !ok {
					for {
						watcher, err := c.clientSet.CoreV1().Nodes().Watch(ctx, opt)
						if err == nil {
							rc = watcher.ResultChan()
							continue loop
						}

						if c.logger != nil {
							c.logger.Printf("Failed to watch nodes: %s", err)
						}
						select {
						case <-ctx.Done():
							break loop
						case <-time.After(time.Second * 5):
						}
					}
				}
				switch event.Type {
				case watch.Added:
					node := event.Object.(*corev1.Node)
					if !c.hasNode(node.Name) {
						ch <- node.Name
					}
				case watch.Deleted:
					node := event.Object.(*corev1.Node)
					c.deleteNode(node.Name)
				}
			case <-ctx.Done():
				watcher.Stop()
				break loop
			}
		}
		if c.logger != nil {
			c.logger.Printf("Stop locking node status")
		}
	}()
	return nil
}

func (c *Controller) lockNodes(ctx context.Context, nodes <-chan string) {
	tasks := newParallelTasks(16)
	for node := range nodes {
		if node == "" {
			continue
		}
		localNode := node
		tasks.Add(func() {
			_, err := c.lockNodeStatus(ctx, localNode)
			if err != nil {
				if c.logger != nil {
					c.logger.Printf("Failed to lock node %s status: %s", localNode, err)
				}
				return
			}
			c.nodePoolMut.Lock()
			c.nodePool[localNode] = struct{}{}
			c.nodePoolMut.Unlock()
			err = c.heartbeatNode(ctx, localNode)
			if err != nil {
				if c.logger != nil {
					c.logger.Printf("Failed to heartbeat node %s: %s", localNode, err)
				}
			}
		})
	}
	tasks.Wait()
	return
}

func (c *Controller) lockNodeStatus(ctx context.Context, nodeName string) (*corev1.Node, error) {
	node, err := c.clientSet.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}
		node = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
			},
		}
		sum, err := toTemplateJson(c.nodeTemplate, node, c.funcMap)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(sum, &node)
		if err != nil {
			return nil, err
		}
		node, err = c.clientSet.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
	}

	err = c.lockNode(ctx, node)
	if err != nil {
		return nil, err
	}
	if c.logger != nil {
		c.logger.Printf("Lock node %s status", nodeName)
	}
	return node, nil
}

func (c *Controller) configurePod(pod *corev1.Pod) (bool, error) {

	// Mark the pod IP that existed before the kubelet was started
	if c.cidrIPNet.Contains(net.ParseIP(pod.Status.PodIP)) {
		c.ipPool.Use(pod.Status.PodIP)
	}

	merge := c.statusTemplate
	if m, ok := pod.Annotations[mergeLabel]; ok && strings.TrimSpace(m) != "" {
		merge = m
	}
	patch, err := toTemplateJson(merge, pod, c.funcMap)
	if err != nil {
		return false, err
	}

	original, err := json.Marshal(pod.Status)
	if err != nil {
		return false, err
	}

	sum, err := strategicpatch.StrategicMergePatch(original, patch, pod.Status)
	if err != nil {
		return false, err
	}

	podStatus := corev1.PodStatus{}
	err = json.Unmarshal(sum, &podStatus)
	if err != nil {
		return false, err
	}

	dist, err := json.Marshal(podStatus)
	if err != nil {
		return false, err
	}

	if bytes.Equal(original, dist) {
		return false, nil
	}
	pod.Status = podStatus
	return true, nil
}

func (c *Controller) configureNode(node *corev1.Node) ([]byte, error) {
	patch, err := toTemplateJson(c.nodeInitializationTemplate, node, c.funcMap)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]json.RawMessage{
		"status": patch,
	})
}

func (c *Controller) configureHeartbeatNode(node *corev1.Node) ([]byte, error) {
	patch, err := toTemplateJson(c.nodeHeartbeatTemplate, node, c.funcMap)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]json.RawMessage{
		"status": patch,
	})
}

func (c *Controller) NodeIP() string {
	return c.nodeIP
}
