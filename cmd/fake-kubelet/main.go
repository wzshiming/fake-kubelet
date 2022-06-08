package main

import (
	"bytes"
	"context"
	"fmt"
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
	cidr                           = getEnv("CIDR", "10.0.0.1/24")
	nodeIP                         = net.ParseIP(getEnv("NODE_IP", "196.168.0.1"))
	nodeName                       = getEnv("NODE_NAME", "fake")
	takeOverAll                    = getEnvBool("TAKE_OVER_ALL", false)
	takeOverLabelsSelector         = getEnv("TAKE_OVER_LABELS_SELECTOR", "type=fake-kubelet")
	generateNodeName               = getEnv("GENERATE_NODE_NAME", "")
	generateReplicas               = getEnv("GENERATE_REPLICAS", "0")
	kubeconfig                     = getEnv("KUBECONFIG", "")
	healthAddress                  = getEnv("HEALTH_ADDRESS", "") // deprecated: use serverAddress instead
	serverAddress                  = getEnv("SERVER_ADDRESS", healthAddress)
	podStatusTemplatePath          = ""
	podStatusTemplate              = getEnv("POD_STATUS_TEMPLATE", templates.DefaultPodStatusTemplate)
	nodeTemplatePath               = ""
	nodeTemplate                   = getEnv("NODE_TEMPLATE", templates.DefaultNodeTemplate)
	nodeHeartbeatTemplateePath     = ""
	nodeHeartbeatTemplate          = getEnv("NODE_HEARTBEAT_TEMPLATE", templates.DefaultNodeHeartbeatTemplate)
	nodeInitializationTemplatePath = ""
	nodeInitializationTemplate     = getEnv("NODE_INITIALIZATION_TEMPLATE", templates.DefaultNodeInitializationTemplate)
	master                         = ""

	logger = log.New(os.Stderr, "[fake-kubelet] ", log.LstdFlags)
)

func init() {
	compatibleFlags()
	pflag.StringVar(&cidr, "cidr", cidr, "CIDR of the pod ip")
	pflag.IPVar(&nodeIP, "node-ip", nodeIP, "IP of the node")
	pflag.StringVarP(&nodeName, "node-name", "n", nodeName, "Names of the node")
	pflag.BoolVar(&takeOverAll, "take-over-all", takeOverAll, "Take over all nodes, there should be no nodes maintained by real Kubelet in the cluster")
	pflag.StringVar(&takeOverLabelsSelector, "take-over-labels-selector", takeOverLabelsSelector, "Selector of nodes to take over")
	pflag.StringVar(&generateNodeName, "generate-node-name", generateNodeName, "Generate node name")
	pflag.StringVar(&generateReplicas, "generate-replicas", generateReplicas, "Generate replicas")
	pflag.StringVar(&kubeconfig, "kubeconfig", kubeconfig, "Path to the kubeconfig file to use")
	pflag.StringVar(&master, "master", master, "Server is the address of the kubernetes cluster")
	pflag.StringVar(&serverAddress, "server-address", serverAddress, "Address to expose health and metrics on")
	pflag.StringVar(&podStatusTemplatePath, "pod-status-template-file", podStatusTemplatePath, "Template for pod status file")
	pflag.StringVar(&nodeTemplatePath, "node-template-file", nodeTemplatePath, "Template for node status file")
	pflag.StringVar(&nodeHeartbeatTemplateePath, "node-heartbeat-template-file", nodeHeartbeatTemplateePath, "Template for node heartbeat status file")
	pflag.StringVar(&nodeInitializationTemplatePath, "node-initialization-template-file", nodeInitializationTemplatePath, "Template for node initialization status file")

	pflag.Parse()

}

func readFile(path string, defaultConetnt string) (string, error) {
	if path == "" {
		return defaultConetnt, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return defaultConetnt, nil
	}
	return string(data), nil
}

// compatibleFlags is used to convert deprecated flags to new flags.
func compatibleFlags() {
	args := make([]string, 0, len(os.Args))
	args = append(args, os.Args[0])
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "--") && strings.Contains(arg, "_") {
			newArg := strings.ReplaceAll(arg, "_", "-")
			fmt.Fprintf(os.Stderr, "WARNING: flag %q is deprecated, please use %q instead\n", arg, newArg)
			arg = newArg
		}
		args = append(args, arg)
	}
	os.Args = args
}

func main() {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	notify.OnceSlice([]os.Signal{syscall.SIGINT, syscall.SIGTERM}, cancel)

	var err error
	if kubeconfig != "" {
		f, err := os.Stat(kubeconfig)
		if err != nil || f.IsDir() {
			kubeconfig = ""
		}
	}

	podStatusTemplate, err = readFile(podStatusTemplatePath, podStatusTemplate)
	if err != nil {
		logger.Fatalf("Failed to read pod status template: %v", err)
	}
	nodeTemplate, err = readFile(nodeTemplatePath, nodeTemplate)
	if err != nil {
		logger.Fatalf("Failed to read node status template: %v", err)
	}
	nodeHeartbeatTemplate, err = readFile(nodeHeartbeatTemplateePath, nodeHeartbeatTemplate)
	if err != nil {
		logger.Fatalf("Failed to read node heartbeat template: %v", err)
	}
	nodeInitializationTemplate, err = readFile(nodeInitializationTemplatePath, nodeInitializationTemplate)
	if err != nil {
		logger.Fatalf("Failed to read node initialization template: %v", err)
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
		logger.Printf("Watch nodes %q", strings.Join(nodes, ","))
		logger.Printf("Watch nodes with labels %q", takeOverLabelsSelector)
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
