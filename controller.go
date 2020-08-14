package fake_kubelet

import (
	"context"
	"encoding/binary"
	"log"
	"net"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

type Controller struct {
	HostIP    net.IP
	NodeIP    net.IP
	Name      string
	ClientSet *kubernetes.Clientset
	ipPool    *ipPool
}

func NewController(clientSet *kubernetes.Clientset, name string, hostIP, nodeIP net.IP) *Controller {
	var index uint64
	n := &Controller{
		ClientSet: clientSet,
		Name:      name,
		HostIP:    hostIP,
		NodeIP:    nodeIP,
		ipPool: &ipPool{
			usable: map[string]struct{}{},
			used:   map[string]struct{}{},
			New: func() string {
				index++
				return addIp(hostIP, index).String()
			},
		},
	}
	return n
}

func (c *Controller) deletePod(ctx context.Context, pod *corev1.Pod) error {
	if len(pod.Finalizers) != 0 {
		pod.Finalizers = nil
		pod.ResourceVersion = "0"
		_, err := c.ClientSet.CoreV1().Pods(pod.Namespace).Update(ctx, pod, metav1.UpdateOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
	}

	err := c.ClientSet.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0))
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
		c.ipPool.Put(pod.Status.PodIP)
		err := c.deletePod(ctx, pod)
		if err != nil {
			return err
		}
		log.Printf("Delete %s.%s", pod.Name, pod.Namespace)
		return nil
	}

	if !c.configurePod(pod) {
		return nil
	}
	pod.ResourceVersion = "0"
	_, err := c.ClientSet.CoreV1().Pods(pod.Namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	log.Printf("Ready %s.%s", pod.Name, pod.Namespace)
	return nil
}

func (c *Controller) LockPodReadyStatus(ctx context.Context) error {
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
			fields.OneTermEqualSelector("spec.nodeName", c.Name),
			fields.OneTermEqualSelector("status.phase", string(corev1.PodPending)),
		).String(),
	}
	err := c.lockPodReadyStatus(ctx, lockCh, lockPendingOpt)
	if err != nil {
		return err
	}

	lockOtherOpt := metav1.ListOptions{
		FieldSelector: fields.AndSelectors(
			fields.OneTermEqualSelector("spec.nodeName", c.Name),
			fields.OneTermNotEqualSelector("status.phase", string(corev1.PodPending)),
		).String(),
	}
	err = c.lockPodReadyStatus(ctx, lockCh, lockOtherOpt)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) lockPodReadyStatus(ctx context.Context, ch chan<- *corev1.Pod, opt metav1.ListOptions) error {
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
						log.Printf("Error get pod %s", err)
						return
					}
					rc = watcher.ResultChan()
					continue
				}
				switch event.Type {
				case watch.Added, watch.Modified:
					pod := event.Object.(*corev1.Pod)
					ch <- pod
				case watch.Deleted:
				default:
				}
			case <-ctx.Done():
				close(ch)
				watcher.Stop()
				log.Printf("stop locking pod ready status in nodes %s", c.Name)
				return
			}

		}
	}()

	return nil
}

func (c *Controller) lockNode(ctx context.Context, node *corev1.Node) error {
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

func (c *Controller) heartbeatNode(ctx context.Context, node *corev1.Node) error {
	node.ResourceVersion = "0"
	updateNodeStatusHeartbeat(node)
	_, err := c.ClientSet.CoreV1().Nodes().UpdateStatus(ctx, node, metav1.UpdateOptions{})
	return err
}

func (c *Controller) LockNodeReadyStatus(ctx context.Context) error {
	node, err := c.ClientSet.CoreV1().Nodes().Get(ctx, c.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	node.ResourceVersion = "0"
	err = c.lockNode(ctx, node)
	if err != nil {
		return err
	}

	selector := metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", c.Name).String(),
	}
	watcher, err := c.ClientSet.CoreV1().Nodes().Watch(ctx, selector)
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
					watcher, err := c.ClientSet.CoreV1().Nodes().Watch(ctx, selector)
					if err != nil {
						log.Printf("Error watch node %s", err)
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
						log.Printf("Error lock node %s", err)
						continue
					}
				}
			case <-th.C:
				th.Reset(heartbeatInterval)
				node, err := c.ClientSet.CoreV1().Nodes().Get(ctx, c.Name, metav1.GetOptions{})
				if err != nil {
					log.Printf("Error get node %s", err)
					continue
				}
				err = c.heartbeatNode(ctx, node)
				if err != nil {
					log.Printf("Error update heartbeat %s", err)
				}
			case <-ctx.Done():
				watcher.Stop()
				log.Printf("Stop locking nodes %s ready status", c.Name)
				return
			}
		}
	}()

	return nil
}

func (c *Controller) configurePod(pod *corev1.Pod) (update bool) {
	now := metav1.Now()
	if pod.Status.Phase != corev1.PodRunning {
		pod.Status.Phase = corev1.PodRunning
		update = true
	}
	if pod.Status.HostIP == "" {
		pod.Status.HostIP = c.NodeIP.String()
		update = true
	}
	if pod.Status.PodIP == "" {
		pod.Status.PodIP = c.ipPool.Get()
		update = true
	} else {
		c.ipPool.Use(pod.Status.PodIP)
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

func (c *Controller) configureNode(n *corev1.Node) (update bool) {
	if n.Status.Phase != corev1.NodeRunning {
		n.Status.Phase = corev1.NodeRunning
		update = true
	}

	capacity := nodeCapacity()
	if !reflect.DeepEqual(n.Status.Capacity, capacity) {
		n.Status.Capacity = capacity
		update = true
	}

	if !reflect.DeepEqual(n.Status.Allocatable, capacity) {
		n.Status.Allocatable = capacity
		update = true
	}

	addresses := c.nodeAddresses()
	if !reflect.DeepEqual(n.Status.Addresses, capacity) {
		n.Status.Addresses = addresses
		update = true
	}

	daemonEndpoints := nodeDaemonEndpoints()
	if !reflect.DeepEqual(n.Status.DaemonEndpoints, daemonEndpoints) {
		n.Status.DaemonEndpoints = daemonEndpoints
		update = true
	}

	info := nodeInfo()
	if !reflect.DeepEqual(n.Status.NodeInfo, info) {
		n.Status.NodeInfo = info
		update = true
	}

	cond := nodeConditions()
	if len(n.Status.Conditions) != len(cond) {
		n.Status.Conditions = cond
		updateNodeStatusHeartbeat(n)
		updateNodeStatusTransition(n)
	}

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
	return []corev1.NodeCondition{
		{
			Type:    "Ready",
			Status:  corev1.ConditionTrue,
			Reason:  "KubeletReady",
			Message: "kubelet is ready.",
		},
		{
			Type:    "OutOfDisk",
			Status:  corev1.ConditionFalse,
			Reason:  "KubeletHasSufficientDisk",
			Message: "kubelet has sufficient disk space available",
		},
		{
			Type:    "MemoryPressure",
			Status:  corev1.ConditionFalse,
			Reason:  "KubeletHasSufficientMemory",
			Message: "kubelet has sufficient memory available",
		},
		{
			Type:    "DiskPressure",
			Status:  corev1.ConditionFalse,
			Reason:  "KubeletHasNoDiskPressure",
			Message: "kubelet has no disk pressure",
		},
		{
			Type:    "NetworkUnavailable",
			Status:  corev1.ConditionFalse,
			Reason:  "RouteCreated",
			Message: "RouteController created a route",
		},
	}
}

func (c *Controller) nodeAddresses() []corev1.NodeAddress {
	return []corev1.NodeAddress{
		{
			Type:    corev1.NodeInternalIP,
			Address: c.NodeIP.String(),
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

func updateNodeStatusTransition(n *corev1.Node) {
	now := metav1.Now()
	for i := range n.Status.Conditions {
		n.Status.Conditions[i].LastTransitionTime = now
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

type ipPool struct {
	New    func() string
	used   map[string]struct{}
	usable map[string]struct{}
}

func (i *ipPool) Get() string {
	ip := ""
	if len(i.usable) != 0 {
		for s := range i.usable {
			ip = s
		}
	}
	if ip == "" && i.New != nil {
		ip = i.New()
	}
	delete(i.usable, ip)
	i.used[ip] = struct{}{}
	return i.New()
}

func (i *ipPool) Put(ip string) {
	delete(i.used, ip)
	i.usable[ip] = struct{}{}
}

func (i *ipPool) Use(ip string) {
	i.used[ip] = struct{}{}
}
