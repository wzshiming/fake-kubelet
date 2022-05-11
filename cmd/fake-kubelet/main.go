package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/pflag"
	fake_kubelet "github.com/wzshiming/fake-kubelet"
	"github.com/wzshiming/fake-kubelet/templates"
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
	takeOverAll                = getEnvBool("TAKE_OVER_ALL", false)
	takeOverLabelsSelector     = getEnv("TAKE_OVER_LABELS_SELECTOR", "type=fake-kubelet")
	generateNodeName           = getEnv("GENERATE_NODE_NAME", "")
	generateReplicas           = getEnv("GENERATE_REPLICAS", "0")
	kubeconfig                 = getEnv("KUBECONFIG", "")
	healthAddress              = getEnv("HEALTH_ADDRESS", "") // deprecated: use serverAddress instead
	serverAddress              = getEnv("SERVER_ADDRESS", healthAddress)
	podStatusTemplate          = getEnv("POD_STATUS_TEMPLATE", templates.DefaultPodStatusTemplate)
	nodeTemplate               = getEnv("NODE_TEMPLATE", templates.DefaultNodeTemplate)
	nodeHeartbeatTemplate      = getEnv("NODE_HEARTBEAT_TEMPLATE", templates.DefaultNodeHeartbeatTemplate)
	nodeInitializationTemplate = getEnv("NODE_INITIALIZATION_TEMPLATE", templates.DefaultNodeInitializationTemplate)
	master                     = ""

	logger = log.New(os.Stderr, "[fake-kubelet] ", log.LstdFlags)
)

func init() {
	pflag.StringVar(&cidr, "cidr", cidr, "cidr")
	pflag.IPVar(&nodeIP, "node_ip", nodeIP, "node ip")
	pflag.StringVarP(&nodeName, "node_name", "n", nodeName, "node name")
	pflag.BoolVar(&takeOverAll, "take_over_all", takeOverAll, "take over all node")
	pflag.StringVar(&takeOverLabelsSelector, "take_over_labels_selector", takeOverLabelsSelector, "take over labels selector")
	pflag.StringVar(&kubeconfig, "kubeconfig", kubeconfig, "kubeconfig")
	pflag.StringVar(&master, "master", master, "master")
	pflag.StringVar(&serverAddress, "server_address", serverAddress, "server address")
	pflag.Parse()
}

func main() {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	notify.OnceSlice([]os.Signal{syscall.SIGINT, syscall.SIGTERM}, cancel)

	if kubeconfig != "" {
		f, err := os.Stat(kubeconfig)
		if err != nil || f.IsDir() {
			kubeconfig = ""
		}
	}
	clientset, err := newClientset(master, kubeconfig)
	if err != nil {
		logger.Fatalln(err)
	}

	var nodes []string
	for _, n := range strings.SplitN(nodeName, ",", -1) {
		if n != "" {
			nodes = append(nodes, n)
		}
	}
	if generateNodeName != "" {
		u, err := strconv.ParseUint(generateReplicas, 10, 64)
		if err == nil {
			for i := 0; i != int(u); i++ {
				nodes = append(nodes, generateNodeName+strconv.Itoa(i))
			}
		}
	}

	if takeOverAll {
		logger.Printf("Watch all nodes")
	} else {
		logger.Printf("Watch fake node %q", nodeName)
	}

	n, err := fake_kubelet.NewController(fake_kubelet.Config{
		ClientSet:                  clientset,
		Nodes:                      nodes,
		TakeOverAll:                takeOverAll,
		TakeOverLabelsSelector:     takeOverLabelsSelector,
		CIDR:                       cidr,
		NodeIP:                     nodeIP.String(),
		Logger:                     logger,
		PodStatusTemplate:          podStatusTemplate,
		NodeTemplate:               nodeTemplate,
		NodeHeartbeatTemplate:      nodeHeartbeatTemplate,
		NodeInitializationTemplate: nodeInitializationTemplate,
	})
	if err != nil {
		logger.Fatalln(err)
	}

	err = n.Start(ctx)
	if err != nil {
		logger.Fatalln(err)
	}

	if serverAddress != "" {
		go Server(ctx, serverAddress)
	}

	<-ctx.Done()
}

func Server(ctx context.Context, address string) {
	promHandler := promhttp.Handler()
	svc := &http.Server{
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
		Addr: address,
		Handler: http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/healthz", "/health":
				rw.Write([]byte("health"))
			case "/metrics":
				promHandler.ServeHTTP(rw, r)
			default:
				http.NotFound(rw, r)
			}
		}),
	}

	err := svc.ListenAndServe()
	if err != nil {
		logger.Fatal("Fatal start server")
	}
}

func newClientset(master, kubeconfig string) (kubernetes.Interface, error) {
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

func getEnvBool(name string, defaults bool) bool {
	val, ok := os.LookupEnv(name)
	if ok {
		boolean, err := strconv.ParseBool(val)
		if err == nil {
			return boolean
		}
	}
	return defaults
}
