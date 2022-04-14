package main

import (
	"context"
	_ "embed"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
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
	takeOverAll                = getEnvBool("TAKE_OVER_ALL", false)
	generateNodeName           = getEnv("GENERATE_NODE_NAME", "")
	generateReplicas           = getEnv("GENERATE_REPLICAS", "0")
	kubeconfig                 = getEnv("KUBECONFIG", "")
	healthAddress              = getEnv("HEALTH_ADDRESS", "")
	statusPodTemplate          = getEnv("POD_STATUS_TEMPLATE", defaultPodStatusTemplate)
	nodeTemplate               = getEnv("NODE_TEMPLATE", defaultNodeTemplate)
	nodeHeartbeatTemplate      = getEnv("NODE_HEARTBEAT_TEMPLATE", defaultNodeHeartbeatTemplate)
	nodeInitializationTemplate = getEnv("NODE_INITIALIZATION_TEMPLATE", defaultNodeInitializationTemplate)
	master                     = ""

	logger = log.New(os.Stderr, "[fake-kubelet] ", log.LstdFlags)
)

func init() {
	pflag.StringVar(&cidr, "cidr", cidr, "cidr")
	pflag.IPVar(&nodeIP, "node_ip", nodeIP, "node ip")
	pflag.StringVarP(&nodeName, "node_name", "n", nodeName, "node name")
	pflag.BoolVar(&takeOverAll, "take_over_all", takeOverAll, "take over all node")
	pflag.StringVar(&kubeconfig, "kubeconfig", kubeconfig, "kubeconfig")
	pflag.StringVar(&master, "master", master, "master")
	pflag.Parse()
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
		ClientSet:                  cliset,
		Nodes:                      nodes,
		TakeOverAll:                takeOverAll,
		CIDR:                       cidr,
		NodeIP:                     nodeIP.String(),
		Logger:                     logger,
		StatusTemplate:             statusPodTemplate,
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
		logger.Fatal("Fatal start health server")
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

var (
	//go:embed pod.status.tpl
	defaultPodStatusTemplate string

	//go:embed node.tpl
	defaultNodeTemplate string

	//go:embed node.heartbeat.tpl
	defaultNodeHeartbeatTemplate string

	//go:embed node.initialization.tpl
	defaultNodeInitializationTemplate string
)
