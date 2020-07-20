package fake_kubelet

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"reflect"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

type Node struct {
	Index     uint64
	HostIP    net.IP
	Name      string
	Provider  string
	ClientSet *kubernetes.Clientset
}

func NewNode(clientSet *kubernetes.Clientset, name string, provider string, hostIP net.IP) *Node {
	return &Node{
		ClientSet: clientSet,
		Name:      name,
		Provider:  provider,
		HostIP:    hostIP,
	}
}

func (c *Node) String() string {
	return fmt.Sprintf("<Node %s>", c.Name)
}

func (c *Node) DeletePodInNode(ctx context.Context) error {
	opt := metav1.ListOptions{
		FieldSelector: c.selector().String(),
	}
	err := c.ClientSet.CoreV1().Pods(corev1.NamespaceAll).DeleteCollection(ctx, *metav1.NewDeleteOptions(0), opt)
	if err != nil {
		return err
	}

	return nil
}

func (c *Node) Delete(ctx context.Context) error {
	err := c.ClientSet.CoreV1().Nodes().Delete(ctx, c.Name, *metav1.NewDeleteOptions(0))
	if err != nil {
		return err
	}

	return nil
}

func (c *Node) Create(ctx context.Context, node *corev1.Node) error {
	node.Name = c.Name

	node.Annotations = map[string]string{
		"node.alpha.kubernetes.io/ttl": "0",
	}
	node.Labels = map[string]string{
		"beta.kubernetes.io/os":   "linux",
		"kubernetes.io/os":        "linux",
		"kubernetes.io/hostname":  c.Name,
		"kubernetes.io/role":      "agent",
		"type":                    "virtual-kubelet",
		"minikube.k8s.io/version": "none",
	}
	node.Spec.Taints = []corev1.Taint{
		{
			Effect: corev1.TaintEffectNoSchedule,
			Key:    "virtual-kubelet.io/provider",
			Value:  c.Provider,
		},
	}

	c.configureNode(node)
	_, err := c.ClientSet.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	return nil
}

func (c *Node) selector() fields.Selector {
	return fields.OneTermEqualSelector("spec.nodeName", c.Name)
}

func (c *Node) deletePod(ctx context.Context, pod *corev1.Pod) error {
	return c.ClientSet.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0))
}

func (c *Node) lockPod(ctx context.Context, pod *corev1.Pod) error {
	if pod.DeletionTimestamp != nil {
		err := c.deletePod(ctx, pod)
		if err != nil {
			return err
		}
		log.Printf("ready %s.%s", pod.Name, pod.Namespace)
		return nil
	}

	if !c.configurePod(pod) {
		return nil
	}
	pod.ResourceVersion = "0"
	_, err := c.ClientSet.CoreV1().Pods(pod.Namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{})
	if err != nil {
		pod, err = c.ClientSet.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if c.configurePod(pod) {
			_, err = c.ClientSet.CoreV1().Pods(pod.Namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}
		log.Printf("ready %s.%s", pod.Name, pod.Namespace)
	}
	return nil
}

func (c *Node) LockPodReadyStatus(ctx context.Context) error {
	opt := metav1.ListOptions{
		FieldSelector: c.selector().String(),
	}

	watcher, err := c.ClientSet.CoreV1().Pods(corev1.NamespaceAll).Watch(ctx, opt)
	if err != nil {
		return err
	}

	go func() {
		rc := watcher.ResultChan()
		for {
			select {
			case event, ok := <-rc:
				if !ok {
					watcher, err := c.ClientSet.CoreV1().Pods(corev1.NamespaceAll).Watch(ctx, opt)
					if err != nil {
						log.Println(err)
						return
					}
					rc = watcher.ResultChan()
					continue
				}
				switch event.Type {
				case watch.Added, watch.Modified:
					pod := event.Object.(*corev1.Pod)
					err := c.lockPod(ctx, pod)
					if err != nil {
						log.Println(err)
						continue
					}
				case watch.Deleted:
				case watch.Error:
					log.Printf("not handle %s", event.Type)
				}
			case <-ctx.Done():
				watcher.Stop()
				log.Printf("stop locking pod ready status in %s", c.String())
				return
			}

		}
	}()

	list, err := c.ClientSet.CoreV1().Pods(corev1.NamespaceAll).List(ctx, opt)
	if err != nil {
		return err
	}
	for _, item := range list.Items {
		err = c.lockPod(ctx, &item)
		if err != nil {
			log.Println(err)
		}
	}
	return nil
}

func (c *Node) lockNode(ctx context.Context, node *corev1.Node) error {
	if node.Name != c.Name {
		return nil
	}
	if !c.configureNode(node) {
		return nil
	}
	node.ResourceVersion = "0"

	_, err := c.ClientSet.CoreV1().Nodes().UpdateStatus(ctx, node, metav1.UpdateOptions{})
	return err
}

func (c *Node) LockReadyStatus(ctx context.Context) error {
	watcher, err := c.ClientSet.CoreV1().Nodes().Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	heartbeatInterval := 10 * time.Second
	th := time.NewTimer(heartbeatInterval)
	go func() {
		rc := watcher.ResultChan()
		for {
			select {
			case event, ok := <-rc:
				if !ok {
					watcher, err := c.ClientSet.CoreV1().Nodes().Watch(ctx, metav1.ListOptions{})
					if err != nil {
						log.Println(err)
						return
					}
					rc = watcher.ResultChan()
					continue
				}
				switch event.Type {
				case watch.Added, watch.Modified:
					node := event.Object.(*corev1.Node)
					err := c.lockNode(ctx, node)
					if err != nil {
						log.Println(err)
						continue
					}
				}
			case <-th.C:
				node, err := c.ClientSet.CoreV1().Nodes().Get(ctx, c.Name, metav1.GetOptions{})
				if err != nil {
					log.Println(err)
					continue
				}
				node.ResourceVersion = "0"
				updateNodeStatusHeartbeat(node)
				_, err = c.ClientSet.CoreV1().Nodes().UpdateStatus(ctx, node, metav1.UpdateOptions{})
				if err != nil {
					log.Println(err)
				}
				th.Reset(heartbeatInterval)
			case <-ctx.Done():
				watcher.Stop()
				log.Printf("stop locking %s ready status", c.String())
				return
			}

		}
	}()

	return nil
}

func (c *Node) configurePod(pod *corev1.Pod) (update bool) {
	now := metav1.Now()
	if pod.Status.Phase != corev1.PodRunning {
		pod.Status.Phase = corev1.PodRunning
		update = true
	}
	if pod.Status.HostIP == "" {
		pod.Status.HostIP = c.HostIP.String()
		update = true
	}
	if pod.Status.PodIP == "" {
		pod.Status.PodIP = addIp(c.HostIP, atomic.AddUint64(&c.Index, 1)).String()
		update = true
	}
	if pod.Status.StartTime.IsZero() {
		pod.Status.StartTime = &now
		update = true
	}

	if len(pod.Status.Conditions) == 0 {
		pod.Status.Conditions = podConditions()
		update = true
	} else if cond := podConditions(); !reflect.DeepEqual(pod.Status.Conditions, cond) {
		pod.Status.Conditions = cond
		update = true
	}

	if len(pod.Status.ContainerStatuses) == 0 {
		containerStatuses := make([]corev1.ContainerStatus, 0, len(pod.Spec.Containers))
		for _, container := range pod.Spec.Containers {
			containerStatuses = append(containerStatuses, corev1.ContainerStatus{
				Name:         container.Name,
				Image:        container.Image,
				Ready:        true,
				RestartCount: 0,
				State: corev1.ContainerState{
					Running: &corev1.ContainerStateRunning{
						StartedAt: now,
					},
				},
			})
		}
		pod.Status.ContainerStatuses = containerStatuses
		update = true
	}
	return update
}

func (c *Node) configureNode(n *corev1.Node) (update bool) {
	if n.Status.Phase != corev1.NodeRunning {
		n.Status.Phase = corev1.NodeRunning
		update = true
	}

	capacity := nodeCapacity()
	if reflect.DeepEqual(n.Status.Capacity, capacity) {
		n.Status.Capacity = capacity
		update = true
	}

	if reflect.DeepEqual(n.Status.Allocatable, capacity) {
		n.Status.Allocatable = capacity
		update = true
	}

	addresses := nodeAddresses()
	if reflect.DeepEqual(n.Status.Addresses, capacity) {
		n.Status.Addresses = addresses
		update = true
	}

	if len(n.Status.Conditions) == 0 {
		n.Status.Conditions = nodeConditions()
		update = true
	}

	daemonEndpoints := nodeDaemonEndpoints()
	if reflect.DeepEqual(n.Status.DaemonEndpoints, daemonEndpoints) {
		n.Status.DaemonEndpoints = daemonEndpoints
		update = true
	}

	info := nodeInfo()
	if reflect.DeepEqual(n.Status.NodeInfo, info) {
		n.Status.NodeInfo = info
		update = true
	}

	n.ObjectMeta.Labels["alpha.service-controller.kubernetes.io/exclude-balancer"] = "true"
	return update
}

func nodeInfo() corev1.NodeSystemInfo {
	return corev1.NodeSystemInfo{
		OperatingSystem: "Linux",
		Architecture:    "amd64",
		KubeletVersion:  "fake",
	}
}

func nodeCapacity() corev1.ResourceList {
	return corev1.ResourceList{
		"cpu":    resource.MustParse("1k"),
		"memory": resource.MustParse("1Ti"),
		"pods":   resource.MustParse("1M"),
	}
}

func podConditions() []corev1.PodCondition {
	return []corev1.PodCondition{
		{
			Type:   corev1.PodInitialized,
			Status: corev1.ConditionTrue,
		},
		{
			Type:   corev1.PodReady,
			Status: corev1.ConditionTrue,
		},
		{
			Type:   corev1.ContainersReady,
			Status: corev1.ConditionTrue,
		},
		{
			Type:   corev1.PodScheduled,
			Status: corev1.ConditionTrue,
		},
	}
}

func nodeConditions() []corev1.NodeCondition {
	now := metav1.Now()
	return []corev1.NodeCondition{
		{
			Type:               "Ready",
			Status:             corev1.ConditionTrue,
			LastHeartbeatTime:  now,
			LastTransitionTime: now,
			Reason:             "KubeletReady",
			Message:            "kubelet is ready.",
		},
		{
			Type:               "OutOfDisk",
			Status:             corev1.ConditionFalse,
			LastHeartbeatTime:  now,
			LastTransitionTime: now,
			Reason:             "KubeletHasSufficientDisk",
			Message:            "kubelet has sufficient disk space available",
		},
		{
			Type:               "MemoryPressure",
			Status:             corev1.ConditionFalse,
			LastHeartbeatTime:  now,
			LastTransitionTime: now,
			Reason:             "KubeletHasSufficientMemory",
			Message:            "kubelet has sufficient memory available",
		},
		{
			Type:               "DiskPressure",
			Status:             corev1.ConditionFalse,
			LastHeartbeatTime:  now,
			LastTransitionTime: now,
			Reason:             "KubeletHasNoDiskPressure",
			Message:            "kubelet has no disk pressure",
		},
		{
			Type:               "NetworkUnavailable",
			Status:             corev1.ConditionFalse,
			LastHeartbeatTime:  now,
			LastTransitionTime: now,
			Reason:             "RouteCreated",
			Message:            "RouteController created a route",
		},
	}
}

func nodeAddresses() []corev1.NodeAddress {
	return []corev1.NodeAddress{
		{
			Type: corev1.NodeInternalIP,
			//	Address: hostIP.String(),
		},
	}
}

func nodeDaemonEndpoints() corev1.NodeDaemonEndpoints {
	return corev1.NodeDaemonEndpoints{
		//KubeletEndpoint: corev1.DaemonEndpoint{
		//	Port: 10250,
		//},
	}
}

func updateNodeStatusHeartbeat(n *corev1.Node) {
	now := metav1.Now()
	for i := range n.Status.Conditions {
		n.Status.Conditions[i].LastHeartbeatTime = now
	}
}

func addIp(ip net.IP, add uint64) net.IP {
	if len(ip) < 8 {
		return ip
	}

	out := make(net.IP, len(ip))
	copy(out, ip)

	i := binary.BigEndian.Uint64(out[len(out)-8:])
	i += add

	binary.BigEndian.PutUint64(out[len(out)-8:], i)
	return out
}
