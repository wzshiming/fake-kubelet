package fake_kubelet

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
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
	mut    sync.Mutex
	New    func() string
	used   map[string]struct{}
	usable map[string]struct{}
}

func (i *ipPool) Get() string {
	i.mut.Lock()
	defer i.mut.Unlock()
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
	i.mut.Lock()
	defer i.mut.Unlock()
	delete(i.used, ip)
	i.usable[ip] = struct{}{}
}

func (i *ipPool) Use(ip string) {
	i.mut.Lock()
	defer i.mut.Unlock()
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
	buf := bufferPool.Get().(*bytes.Buffer)
	defer bufferPool.Put(buf)

	buf.Reset()
	err := json.NewEncoder(buf).Encode(original)
	if err != nil {
		return nil, err
	}

	var data interface{}
	decoder := json.NewDecoder(buf)
	decoder.UseNumber()
	err = decoder.Decode(&data)
	if err != nil {
		return nil, err
	}

	buf.Reset()
	err = temp.Execute(buf, data)
	if err != nil {
		return nil, err
	}
	out, err := yaml.YAMLToJSON(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, buf.String())
	}
	return out, nil
}

var (
	templateCache = sync.Map{}
	bufferPool    = sync.Pool{
		New: func() interface{} {
			return &bytes.Buffer{}
		},
	}
)
