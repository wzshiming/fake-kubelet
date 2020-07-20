package main

import (
	"context"
	"log"
	"net"
	"os"

	"github.com/spf13/pflag"
	fake_kubelet "github.com/wzshiming/fake-kubelet"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/deprecated/scheme"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/flowcontrol"
)

var (
	hostIP     = net.ParseIP(getEnv("HOST_IP", "10.0.0.1"))
	nodeName   = getEnv("NODE_NAME", "fake")
	provider   = getEnv("PROVIDER", "fake")
	kubeconfig = ""
	master     = ""
)

func init() {
	pflag.IPVarP(&hostIP, "host_ip", "h", hostIP, "host ip")
	pflag.StringVarP(&nodeName, "node_name", "n", nodeName, "node name")
	pflag.StringVarP(&provider, "provider", "p", provider, "provider name")
	pflag.StringVar(&kubeconfig, "kubeconfig", kubeconfig, "kubeconfig")
	pflag.StringVar(&master, "master", master, "master")
}

func main() {
	ctx := context.Background()
	if kubeconfig != "" {
		f, err := os.Stat(kubeconfig)
		if err != nil || f.IsDir() {
			kubeconfig = ""
		}
	}
	cliset, err := newClientset(master, kubeconfig)
	if err != nil {
		log.Println(err)
		return
	}
	n := fake_kubelet.NewNode(cliset, nodeName, provider, hostIP)
	err = n.Create(ctx, &v1.Node{})
	if err != nil {
		log.Println(err)
	}
	err = n.LockReadyStatus(context.Background())
	if err != nil {
		log.Println(err)
		return
	}

	err = n.LockPodReadyStatus(context.Background())
	if err != nil {
		log.Println(err)
		return
	}
	log.Printf("watch fake nodes %q", nodeName)
	<-ctx.Done()
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
