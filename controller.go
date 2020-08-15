package fake_kubelet

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"reflect"
	"strings"
	"text/template"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

const mergeLabel = "fake/status"

type Controller struct {
	cidrIP                     net.IP
	cidrIPNet                  *net.IPNet
	nodeIP                     net.IP
	name                       string
	clientSet                  *kubernetes.Clientset
	ipPool                     *ipPool
	statusTemplate             string
	nodeHeartbeatTemplate      string
	nodeInitializationTemplate string
	funcMap                    template.FuncMap
}

func NewController(clientSet *kubernetes.Clientset, name string, cidrIP net.IP, cidrIPNet *net.IPNet, nodeIP net.IP, statusTemplate, nodeHeartbeatTemplate, nodeInitializationTemplate string) *Controller {
	var index uint64
	startTime := time.Now().Format(time.RFC3339)
	node := nodeIP.String()
	n := &Controller{
		clientSet: clientSet,
		name:      name,
		cidrIP:    cidrIP,
		cidrIPNet: cidrIPNet,
		nodeIP:    nodeIP,
		ipPool: &ipPool{
			usable: map[string]struct{}{},
			used:   map[string]struct{}{},
			New: func() string {
				index++
				return addIp(cidrIP, index).String()
			},
		},
		statusTemplate:             statusTemplate,
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
			return node
		},
		"PodIP": func() string {
			return n.ipPool.Get()
		},
	}

	return n
}

var (
	removeFinalizers = []byte(`{"metadata":{"finalizers":null}}`)
	deleteOpt        = *metav1.NewDeleteOptions(0)
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
		log.Printf("Delete %s.%s", pod.Name, pod.Namespace)
		return nil
	}

	ok, err := c.configurePod(pod)
	if err != nil {
		return err
	}
	if !ok {
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
	log.Printf("Lock %s.%s", pod.Name, pod.Namespace)
	return nil
}

func (c *Controller) LockPodStatus(ctx context.Context) error {
	lockCh := make(chan *corev1.Pod)
	go func() {
		for {
			select {
			case pod, ok := <-lockCh:
				if !ok {
					return
				}
				err := c.lockPod(ctx, pod)
				if err != nil {
					log.Printf("Error lock pod %s", err)
					continue
				}
			case <-ctx.Done():
				return
			}

		}
	}()

	lockPendingOpt := metav1.ListOptions{
		FieldSelector: fields.AndSelectors(
			fields.OneTermEqualSelector("spec.nodeName", c.name),
			fields.OneTermEqualSelector("status.phase", string(corev1.PodPending)),
		).String(),
	}
	err := c.lockPodStatus(ctx, lockCh, lockPendingOpt)
	if err != nil {
		return err
	}

	lockOtherOpt := metav1.ListOptions{
		FieldSelector: fields.AndSelectors(
			fields.OneTermEqualSelector("spec.nodeName", c.name),
			fields.OneTermNotEqualSelector("status.phase", string(corev1.PodPending)),
		).String(),
	}
	err = c.lockPodStatus(ctx, lockCh, lockOtherOpt)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) lockPodStatus(ctx context.Context, ch chan<- *corev1.Pod, opt metav1.ListOptions) error {
	watcher, err := c.clientSet.CoreV1().Pods(corev1.NamespaceAll).Watch(ctx, opt)
	if err != nil {
		return err
	}

	go func() {
		rc := watcher.ResultChan()
		for {
			select {
			case event, ok := <-rc:
				if !ok {
					watcher, err := c.clientSet.CoreV1().Pods(corev1.NamespaceAll).Watch(ctx, opt)
					if err != nil {
						log.Fatalf("Error get pod %s", err)
						return
					}
					rc = watcher.ResultChan()
					continue
				}
				switch event.Type {
				case watch.Added:
					pod := event.Object.(*corev1.Pod)
					ch <- pod
				case watch.Modified:
					pod := event.Object.(*corev1.Pod)
					if pod.DeletionTimestamp != nil {
						ch <- pod
					}
				case watch.Deleted:
				default:
				}
			case <-ctx.Done():
				watcher.Stop()
				log.Printf("Stop locking pod ready status in nodes %s", c.name)
				return
			}
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

func (c *Controller) heartbeatNode(ctx context.Context, node *corev1.Node) error {
	patch, err := c.configureHeartbeatNode(node)
	if err != nil {
		return err
	}
	_, err = c.clientSet.CoreV1().Nodes().PatchStatus(ctx, node.Name, patch)
	return err
}

func (c *Controller) LockNodeStatus(ctx context.Context) error {
	node, err := c.clientSet.CoreV1().Nodes().Get(ctx, c.name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	err = c.lockNode(ctx, node)
	if err != nil {
		return err
	}

	err = c.heartbeatNode(ctx, node)
	if err != nil {
		return err
	}

	heartbeatInterval := 30 * time.Second
	th := time.NewTimer(heartbeatInterval)
	go func() {
		for {
			select {
			case <-th.C:
				th.Reset(heartbeatInterval)
				err = c.heartbeatNode(ctx, node)
				if err != nil {
					log.Printf("Error update heartbeat %s", err)
				}
			case <-ctx.Done():
				log.Printf("Stop locking nodes %s ready status", c.name)
				return
			}
		}
	}()
	return nil
}

func (c *Controller) configurePod(pod *corev1.Pod) (bool, error) {
	merge := c.statusTemplate
	if m, ok := pod.Annotations[mergeLabel]; ok && strings.TrimSpace(m) != "" {
		merge = m
	}
	patch, err := toTemplateJson(merge, pod, c.funcMap)
	if err != nil {
		return false, err
	}

	podStatus := corev1.PodStatus{}
	err = json.Unmarshal(patch, &podStatus)
	if err != nil {
		return false, err
	}

	if reflect.DeepEqual(podStatus, pod.Status) {
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
	return c.nodeIP.String()
}
