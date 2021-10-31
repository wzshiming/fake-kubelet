package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/pflag"
	fake_kubelet "github.com/wzshiming/fake-kubelet"
	"github.com/wzshiming/notify"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/flowcontrol"
)

var (
	cidr                       = getEnv("CIDR", "10.0.0.1/24")
	nodeIP                     = net.ParseIP(getEnv("NODE_IP", "196.168.0.1"))
	nodeName                   = getEnv("NODE_NAME", "fake")
	kubeconfig                 = getEnv("KUBECONFIG", "")
	healthAddress              = getEnv("HEALTH_ADDRESS", "")
	statusPodTemplate          = getEnv("POD_STATUS_TEMPLATE", defaultPodStatusTemplate)
	nodeHeartbeatTemplate      = getEnv("NODE_HEARTBEAT_TEMPLATE", defaultNodeHeartbeatTemplate)
	nodeInitializationTemplate = getEnv("NODE_INITIALIZATION_TEMPLATE", defaultNodeInitializationTemplate)
	master                     = ""

	cidrIPNet *net.IPNet
	cidrIP    net.IP
)

func init() {
	pflag.StringVar(&cidr, "cidr", cidr, "cidr")
	pflag.IPVar(&nodeIP, "node_ip", nodeIP, "node ip")
	pflag.StringVarP(&nodeName, "node_name", "n", nodeName, "node name")
	pflag.StringVar(&kubeconfig, "kubeconfig", kubeconfig, "kubeconfig")
	pflag.StringVar(&master, "master", master, "master")
	pflag.Parse()
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		pflag.PrintDefaults()
		log.Fatal(err)
	}
	cidrIPNet = ipnet
	cidrIP = ip
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	notify.Once(os.Interrupt, cancel)

	if kubeconfig != "" {
		f, err := os.Stat(kubeconfig)
		if err != nil || f.IsDir() {
			kubeconfig = ""
		}
	}
	cliset, err := newClientset(master, kubeconfig)
	if err != nil {
		log.Fatalln(err)
	}
	n := fake_kubelet.NewController(cliset, strings.SplitN(nodeName, ",", -1), cidrIP, cidrIPNet, nodeIP, statusPodTemplate, nodeHeartbeatTemplate, nodeInitializationTemplate)

	fake_kubelet.RunGrpcServer(n)

	err = n.LockNodeStatus(ctx)
	if err != nil {
		log.Fatalln(err)
	}

	log.Printf("Watch fake nodes %q", nodeName)

	err = n.LockPodStatus(ctx)
	if err != nil {
		log.Fatalln(err)
	}

	if healthAddress != "" {
		go healthServe(healthAddress, ctx)
	}

	<-ctx.Done()
}

func healthServe(address string, ctx context.Context) {
	svc := &http.Server{
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
		Addr: address,
		Handler: http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				http.NotFound(rw, r)
				return
			}
			rw.Write([]byte("health"))
		}),
	}

	err := svc.ListenAndServe()
	if err != nil {
		log.Fatal("Fatal start health server")
	}
}

func newClientset(master, kubeconfig string) (*kubernetes.Clientset, error) {
	cfg, err := clientcmd.BuildConfigFromFlags(master, kubeconfig)
	if err != nil {
		return nil, err
	}
	err = setConfigDefaults(cfg)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

func setConfigDefaults(config *rest.Config) error {
	config.RateLimiter = flowcontrol.NewFakeAlwaysRateLimiter()
	return rest.SetKubernetesDefaults(config)
}

func getEnv(name string, defaults string) string {
	val, ok := os.LookupEnv(name)
	if ok {
		return val
	}
	return defaults
}

var (
	defaultPodStatusTemplate = `
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
`

	defaultNodeHeartbeatTemplate = `
conditions:
- lastHeartbeatTime: {{ Now }}
  lastTransitionTime: {{ StartTime }}
  message: kubelet is ready.
  reason: KubeletReady
  status: "True"
  type: Ready
- lastHeartbeatTime: {{ Now }}
  lastTransitionTime: {{ StartTime }}
  message: kubelet has sufficient disk space available
  reason: KubeletHasSufficientDisk
  status: "False"
  type: OutOfDisk
- lastHeartbeatTime: {{ Now }}
  lastTransitionTime: {{ StartTime }}
  message: kubelet has sufficient memory available
  reason: KubeletHasSufficientMemory
  status: "False"
  type: MemoryPressure
- lastHeartbeatTime: {{ Now }}
  lastTransitionTime: {{ StartTime }}
  message: kubelet has no disk pressure
  reason: KubeletHasNoDiskPressure
  status: "False"
  type: DiskPressure
- lastHeartbeatTime: {{ Now }}
  lastTransitionTime: {{ StartTime }}
  message: RouteController created a route
  reason: RouteCreated
  status: "False"
  type: NetworkUnavailable
`

	defaultNodeInitializationTemplate = `
addresses:
- address: {{ NodeIP }}
  type: InternalIP
allocatable:
  cpu: 1k
  memory: 1Ti
  pods: 1M
capacity:
  cpu: 1k
  memory: 1Ti
  pods: 1M
daemonEndpoints:
  kubeletEndpoint:
    Port: 0
nodeInfo:
  architecture: amd64
  bootID: ""
  containerRuntimeVersion: ""
  kernelVersion: ""
  kubeProxyVersion: ""
  kubeletVersion: fake
  machineID: ""
  operatingSystem: Linux
  osImage: ""
  systemUUID: ""
phase: Running
`
)
