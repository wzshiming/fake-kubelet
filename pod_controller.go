package fake_kubelet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"text/template"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/pager"
)

var (
	removeFinalizers = []byte(`{"metadata":{"finalizers":null}}`)
	deleteOpt        = *metav1.NewDeleteOptions(0)
	podFieldSelector = fields.OneTermNotEqualSelector("spec.nodeName", "").String()
)

// PodController is a fake pods implementation that can be used to test
type PodController struct {
	clientSet         kubernetes.Interface
	nodeIP            string
	cidrIPNet         *net.IPNet
	nodeHasFunc       func(nodeName string) bool
	ipPool            *ipPool
	podStatusTemplate string
	funcMap           template.FuncMap
	logger            Logger
	podChan           chan *corev1.Pod
}

// PodControllerConfig is the configuration for the PodController
type PodControllerConfig struct {
	ClientSet         kubernetes.Interface
	NodeIP            string
	CIDR              string
	NodeHasFunc       func(nodeName string) bool
	PodStatusTemplate string
	Logger            Logger
	FuncMap           template.FuncMap
}

// NewPodController creates a new fake pods controller
func NewPodController(conf PodControllerConfig) (*PodController, error) {
	cidrIPNet, err := parseCIDR(conf.CIDR)
	if err != nil {
		return nil, err
	}
	n := &PodController{
		clientSet:         conf.ClientSet,
		nodeIP:            conf.NodeIP,
		cidrIPNet:         cidrIPNet,
		ipPool:            newIPPool(cidrIPNet),
		nodeHasFunc:       conf.NodeHasFunc,
		logger:            conf.Logger,
		podStatusTemplate: conf.PodStatusTemplate,
		podChan:           make(chan *corev1.Pod),
	}
	n.funcMap = template.FuncMap{
		"NodeIP": func() string {
			return n.nodeIP
		},
		"PodIP": func() string {
			return n.ipPool.Get()
		},
	}
	for k, v := range conf.FuncMap {
		n.funcMap[k] = v
	}
	return n, nil
}

// Start starts the fake pod controller
// It will modify the node status to we want
func (c *PodController) Start(ctx context.Context) error {
	go c.LockPods(ctx, c.podChan)

	opt := metav1.ListOptions{
		FieldSelector: podFieldSelector,
	}
	err := c.WatchPods(ctx, c.podChan, opt)
	if err != nil {
		return fmt.Errorf("failed watch pods: %w", err)
	}
	go func() {
		err = c.ListPods(ctx, c.podChan, opt)
		if err != nil {
			if c.logger != nil {
				c.logger.Printf("failed list pods: %s", err)
			}
		}
	}()
	return nil
}

func (c *PodController) deletePod(ctx context.Context, pod *corev1.Pod) error {
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

// LockPod locks a given pod
func (c *PodController) LockPod(ctx context.Context, pod *corev1.Pod) error {
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
			c.logger.Printf("Skip pod %s.%s on %s: do not need to modify", pod.Name, pod.Namespace, pod.Spec.NodeName)
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

// LockPods locks a pods from the channel
func (c *PodController) LockPods(ctx context.Context, pods <-chan *corev1.Pod) {
	tasks := newParallelTasks(16)
	for pod := range pods {
		localPod := pod
		tasks.Add(func() {
			err := c.LockPod(ctx, localPod)
			if err != nil {
				if c.logger != nil {
					c.logger.Printf("Failed to lock pod %s.%s on %s: %s", localPod.Name, localPod.Namespace, localPod.Spec.NodeName, err)
				}
			}
		})
	}
	tasks.Wait()
}

// WatchPods watch pods put into the channel
func (c *PodController) WatchPods(ctx context.Context, ch chan<- *corev1.Pod, opt metav1.ListOptions) error {
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
					if c.nodeHasFunc(pod.Spec.NodeName) {
						ch <- pod.DeepCopy()
					} else {
						if c.logger != nil {
							c.logger.Printf("Skip pod %s.%s on %s: not take over", pod.Name, pod.Namespace, pod.Spec.NodeName)
						}
					}
				case watch.Modified:
					pod := event.Object.(*corev1.Pod)

					// At a Kubelet, we need to delete this pod on the node we take over
					if pod.DeletionTimestamp != nil {
						if c.nodeHasFunc(pod.Spec.NodeName) {
							ch <- pod.DeepCopy()
						} else {
							if c.logger != nil {
								c.logger.Printf("Skip pod %s.%s on %s: not take over", pod.Name, pod.Namespace, pod.Spec.NodeName)
							}
						}
					}
				case watch.Deleted:
					// Pod is deleted, do nothing
				}
			case <-ctx.Done():
				watcher.Stop()
				break loop
			}
		}
		if c.logger != nil {
			c.logger.Printf("Stop watch pods")
		}
	}()

	return nil
}

// ListPods list pods put into the channel
func (c *PodController) ListPods(ctx context.Context, ch chan<- *corev1.Pod, opt metav1.ListOptions) error {
	listPager := pager.New(func(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
		return c.clientSet.CoreV1().Pods(corev1.NamespaceAll).List(ctx, opts)
	})
	return listPager.EachListItem(ctx, opt, func(obj runtime.Object) error {
		pod := obj.(*corev1.Pod)
		if c.nodeHasFunc(pod.Spec.NodeName) {
			ch <- pod
		}
		return nil
	})
}

// LockPodsOnNode locks pods on the node
func (c *PodController) LockPodsOnNode(ctx context.Context, nodeName string) error {
	return c.ListPods(ctx, c.podChan, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("spec.nodeName", nodeName).String(),
	})
}

func (c *PodController) configurePod(pod *corev1.Pod) (bool, error) {

	// Mark the pod IP that existed before the kubelet was started
	if c.cidrIPNet.Contains(net.ParseIP(pod.Status.PodIP)) {
		c.ipPool.Use(pod.Status.PodIP)
	}

	merge := c.podStatusTemplate
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
