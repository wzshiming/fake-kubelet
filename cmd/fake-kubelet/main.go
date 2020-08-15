package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/spf13/pflag"
	fake_kubelet "github.com/wzshiming/fake-kubelet"
	"github.com/wzshiming/notify"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/deprecated/scheme"
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
	n := fake_kubelet.NewController(cliset, nodeName, cidrIP, cidrIPNet, nodeIP, statusPodTemplate, nodeHeartbeatTemplate, nodeInitializationTemplate)

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
	config.GroupVersion = &schema.GroupVersion{Group: "", Version: "v1"}
	if config.APIPath == "" {
		config.APIPath = "/api"
	}
	if config.NegotiatedSerializer == nil {
		config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	}
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
conditions:
- lastTransitionTime: {{ Now }}
  status: "True"
  type: Initialized
- lastTransitionTime: {{ Now }}
  status: "True"
  type: Ready
- lastTransitionTime: {{ Now }}
  status: "True"
  type: ContainersReady
- lastTransitionTime: {{ Now }}
  status: "True"
  type: PodScheduled
{{ range .Spec.ReadinessGates }}
- lastTransitionTime: {{ Now }}
  status: "True"
  type: {{ .ConditionType }}
{{ end }}
{{ if .Spec.Containers }}
containerStatuses:
{{ range .Spec.Containers }}
- image: {{ .Image }}
  name: {{ .Name }}
  ready: true
  restartCount: 0
  started: true
  state:
    running:
      startedAt: {{ Now }}
{{ end }}
{{ end }}
{{ if .Spec.InitContainers }}
initContainerStatuses:
{{ range .Spec.InitContainers }}
- image: {{ .Image }}
  name: {{ .Name }}
  ready: true
  restartCount: 0
  state:
    terminated:
      exitCode: 0
      finishedAt: {{ Now }}
      reason: Completed
      startedAt: {{ Now }}
{{ end }}
{{ end }}
phase: Running
startTime: {{ Now }}
hostIP: {{ if .Status.HostIP }}{{ .Status.HostIP }}{{ else }}{{ NodeIP }}{{ end }}
podIP: {{ if .Status.PodIP }}{{ .Status.PodIP }}{{ else }}{{ PodIP }}{{ end }}
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
