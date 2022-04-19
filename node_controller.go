package fake_kubelet

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"text/template"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/pager"
)

// NodeController is a fake nodes implementation that can be used to test
type NodeController struct {
	clientSet                kubernetes.Interface
	nodeIP                   string
	nodeSelectorFunc         func(node *corev1.Node) bool
	lockPodsOnNodeFunc       func(nodeName string) error
	nodes                    []string
	nodesSets                *stringSets
	nodeTemplate             string
	nodeHeartbeatTemplate    string
	nodeStatusTemplate       string
	funcMap                  template.FuncMap
	logger                   Logger
	nodeHeartbeatInterval    time.Duration
	nodeHeartbeatParallelism int
	lockNodeParallelism      int
	nodeChan                 chan string
}

// NodeControllerConfig is the configuration for the NodeController
type NodeControllerConfig struct {
	ClientSet                  kubernetes.Interface
	NodeSelectorFunc           func(node *corev1.Node) bool
	LockPodsOnNodeFunc         func(nodeName string) error
	NodeIP                     string
	Nodes                      []string
	NodeTemplate               string
	NodeInitializationTemplate string
	NodeHeartbeatTemplate      string
	Logger                     Logger
	NodeHeartbeatInterval      time.Duration
	NodeHeartbeatParallelism   int
	LockNodeParallelism        int
	FuncMap                    template.FuncMap
}

// NewNodeController creates a new fake nodes controller
func NewNodeController(conf NodeControllerConfig) (*NodeController, error) {
	n := &NodeController{
		clientSet:                conf.ClientSet,
		nodes:                    conf.Nodes,
		nodeSelectorFunc:         conf.NodeSelectorFunc,
		lockPodsOnNodeFunc:       conf.LockPodsOnNodeFunc,
		nodeIP:                   conf.NodeIP,
		nodesSets:                newStringSets(),
		logger:                   conf.Logger,
		nodeTemplate:             conf.NodeTemplate,
		nodeHeartbeatTemplate:    conf.NodeHeartbeatTemplate,
		nodeStatusTemplate:       conf.NodeHeartbeatTemplate + "\n" + conf.NodeInitializationTemplate,
		nodeHeartbeatInterval:    conf.NodeHeartbeatInterval,
		nodeHeartbeatParallelism: conf.NodeHeartbeatParallelism,
		lockNodeParallelism:      conf.LockNodeParallelism,
		nodeChan:                 make(chan string),
	}
	n.funcMap = template.FuncMap{
		"NodeIP": func() string {
			return n.nodeIP
		},
	}
	for k, v := range conf.FuncMap {
		n.funcMap[k] = v
	}

	return n, nil
}

// Start starts the fake nodes controller
// It will create and take over the nodes and keep them alive
// if nodeSelectorFunc is not nil, it will use it to determine if the node should be taken over
func (c *NodeController) Start(ctx context.Context) error {
	go c.KeepNodeHeartbeat(ctx)

	go c.LockNodes(ctx, c.nodeChan)

	for _, node := range c.nodes {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.nodeChan <- node
	}

	if c.nodeSelectorFunc != nil {
		opt := metav1.ListOptions{}
		err := c.WatchNodes(ctx, c.nodeChan, opt)
		if err != nil {
			return fmt.Errorf("failed watch node: %w", err)
		}

		go func() {
			err = c.ListNodes(ctx, c.nodeChan, opt)
			if err != nil {
				if c.logger != nil {
					c.logger.Printf("failed list node: %s", err)
				}
			}
		}()
	} else {
		close(c.nodeChan)
	}
	return nil
}

func (c *NodeController) heartbeatNode(ctx context.Context, nodeName string) error {
	var node corev1.Node
	node.Name = nodeName
	patch, err := c.configureHeartbeatNode(&node)
	if err != nil {
		return err
	}
	_, err = c.clientSet.CoreV1().Nodes().PatchStatus(ctx, node.Name, patch)
	if err != nil {
		return err
	}
	return nil
}

func (c *NodeController) allHeartbeatNode(ctx context.Context, nodes []string, tasks *parallelTasks) {
	for _, node := range nodes {
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
}

// KeepNodeHeartbeat keep node heartbeat
func (c *NodeController) KeepNodeHeartbeat(ctx context.Context) {
	th := time.NewTimer(c.nodeHeartbeatInterval)
	tasks := newParallelTasks(c.nodeHeartbeatParallelism)
	var heartbeatStartTime time.Time
	var nodes []string
loop:
	for {
		select {
		case <-th.C:
			nodes = nodes[:0]
			c.nodesSets.Foreach(func(node string) {
				nodes = append(nodes, node)
			})
			sort.Strings(nodes)
			if c.logger != nil {
				heartbeatStartTime = time.Now()
			}
			c.allHeartbeatNode(ctx, nodes, tasks)
			tasks.Wait()
			if c.logger != nil {
				c.logger.Printf("Heartbeat %d nodes took %s", len(nodes), time.Since(heartbeatStartTime))
			}
			th.Reset(c.nodeHeartbeatInterval)
		case <-ctx.Done():
			if c.logger != nil {
				c.logger.Printf("Stop keep nodes heartbeat")
			}
			break loop
		}
	}
	tasks.Wait()
}

// WatchNodes watch nodes put into the channel
func (c *NodeController) WatchNodes(ctx context.Context, ch chan<- string, opt metav1.ListOptions) error {
	if c.nodeSelectorFunc == nil {
		return nil
	}

	// Watch nodes in the cluster
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
					if !c.nodesSets.Has(node.Name) && c.nodeSelectorFunc(node) {
						ch <- node.Name
					}
				case watch.Modified:
					// node is modified, do nothing
				case watch.Deleted:
					node := event.Object.(*corev1.Node)
					if c.nodesSets.Has(node.Name) && c.nodeSelectorFunc(node) {
						c.nodesSets.Delete(node.Name)
					}
				}
			case <-ctx.Done():
				watcher.Stop()
				break loop
			}
		}
		if c.logger != nil {
			c.logger.Printf("Stop watch nodes")
		}
	}()
	return nil
}

// ListNodes list nodes put into the channel
func (c *NodeController) ListNodes(ctx context.Context, ch chan<- string, opt metav1.ListOptions) error {
	if c.nodeSelectorFunc == nil {
		return nil
	}

	listPager := pager.New(func(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
		return c.clientSet.CoreV1().Nodes().List(ctx, opts)
	})
	return listPager.EachListItem(ctx, opt, func(obj runtime.Object) error {
		node := obj.(*corev1.Node)
		if !c.nodesSets.Has(node.Name) && c.nodeSelectorFunc(node) {
			ch <- node.Name
		}
		return nil
	})
}

// LockNodes locks a nodes from the channel
// if they don't exist we create them and then take over them
// if they exist we take over them
func (c *NodeController) LockNodes(ctx context.Context, nodes <-chan string) {
	tasks := newParallelTasks(c.lockNodeParallelism)
	for node := range nodes {
		if node == "" || c.nodesSets.Has(node) {
			continue
		}
		localNode := node
		tasks.Add(func() {
			c.nodesSets.Put(localNode)
			_, err := c.LockNode(ctx, localNode)
			if err != nil {
				if c.logger != nil {
					c.logger.Printf("Failed to lock node %s: %s", localNode, err)
				}
				return
			}
			if c.lockPodsOnNodeFunc != nil {
				err = c.lockPodsOnNodeFunc(localNode)
				if err != nil {
					if c.logger != nil {
						c.logger.Printf("Failed to lock pods on node %s: %s", localNode, err)
					}
					return
				}
			}
		})
	}
	tasks.Wait()
	return
}

// LockNode locks a given node
func (c *NodeController) LockNode(ctx context.Context, nodeName string) (*corev1.Node, error) {
	node, err := c.clientSet.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}
		node, err = c.newNode(nodeName)
		if err != nil {
			return nil, err
		}
		node, err = c.clientSet.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
		if c.logger != nil {
			c.logger.Printf("Created node %s", nodeName)
		}
		return node, nil
	}

	patch, err := c.configureNode(node)
	if err != nil {
		return nil, err
	}
	node, err = c.clientSet.CoreV1().Nodes().PatchStatus(ctx, node.Name, patch)
	if err != nil {
		return nil, err
	}
	if c.logger != nil {
		c.logger.Printf("Lock node %s", nodeName)
	}
	return node, nil
}

func (c *NodeController) newNode(nodeName string) (*corev1.Node, error) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
	}
	spec, err := toTemplateJson(c.nodeTemplate, node, c.funcMap)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(spec, &node)
	if err != nil {
		return nil, err
	}
	status, err := toTemplateJson(c.nodeStatusTemplate, node, c.funcMap)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(status, &node.Status)
	if err != nil {
		return nil, err
	}
	return node, nil
}

func (c *NodeController) configureNode(node *corev1.Node) ([]byte, error) {
	patch, err := toTemplateJson(c.nodeStatusTemplate, node, c.funcMap)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]json.RawMessage{
		"status": patch,
	})
}

func (c *NodeController) configureHeartbeatNode(node *corev1.Node) ([]byte, error) {
	patch, err := toTemplateJson(c.nodeHeartbeatTemplate, node, c.funcMap)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]json.RawMessage{
		"status": patch,
	})
}

func (c *NodeController) Has(nodeName string) bool {
	return c.nodesSets.Has(nodeName)
}
