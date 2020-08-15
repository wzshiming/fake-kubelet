package fake_kubelet

import (
	"bytes"
	"encoding/binary"
	"net"
	"strings"
	"sync"
	"text/template"

	"sigs.k8s.io/yaml"
)

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

func toTemplateJson(text string, original interface{}, funcMap template.FuncMap) ([]byte, error) {
	text = strings.TrimSpace(text)
	v, ok := templateCache.Load(text)
	if !ok {
		temp, err := template.New("_").Funcs(funcMap).Parse(text)
		if err != nil {
			return nil, err
		}
		templateCache.Store(text, temp)
		v = temp
	}
	temp := v.(*template.Template)

	buf := bytes.Buffer{}
	err := temp.Execute(&buf, original)
	if err != nil {
		return nil, err
	}
	return yaml.YAMLToJSON(buf.Bytes())
}

var templateCache = sync.Map{}
