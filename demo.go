package main

import (
	"encoding/json"
	"fmt"
	"github.com/caoyingjunz/pixiulib/config"
	"go-learning/practise/image-practise/image"
	"k8s.io/klog/v2"
	"strings"
)

func main() {
	c := config.New()
	c.SetConfigFile("./config.yaml")
	c.SetConfigType("yaml")
	var cfg image.Config

	if err := c.Binding(&cfg); err != nil {
		klog.Fatal(err)
	}
	img := image.Image{Cfg: cfg}
	cmd := []string{"sudo", "apt-get", "install", "-y", fmt.Sprintf("kubeadm=%s-00", img.Cfg.Kubernetes.Version[1:])}
	fmt.Println(cmd)

	jsonStr := `{
  "clientVersion": {
    "major": "1",
    "minor": "22",
    "gitVersion": "v1.22.8",
    "gitCommit": "7061dbbf75f9f82e8ab21f9be7e8ffcaae8e0d44",
    "gitTreeState": "clean",
    "buildDate": "2022-03-16T14:08:54Z",
    "goVersion": "go1.16.15",
    "compiler": "gc",
    "platform": "linux/amd64"
  }
}`
	out := []byte(jsonStr)

	// 把 kubeadm 的版本信息赋值给 kubeadmVersion
	var kubeadmVersion image.KubeadmVersion
	// 把 out 中的内容赋值给 kubeadmVersion，
	if err := json.Unmarshal(out, &kubeadmVersion); err != nil {
		fmt.Errorf("failed to unmarshal kubeadm version %v", err)
	}
	klog.V(2).Infof("kubeadmVersion %+v", kubeadmVersion)
	fmt.Println(kubeadmVersion.ClientVersion.GitVersion)

	jsonstrImage := `{
    "kind": "Images",
    "apiVersion": "output.kubeadm.k8s.io/v1alpha1",
    "images": [
        "k8s.gcr.io/kube-apiserver:v1.22.8",
        "k8s.gcr.io/kube-controller-manager:v1.22.8",
        "k8s.gcr.io/kube-scheduler:v1.22.8",
        "k8s.gcr.io/kube-proxy:v1.22.8",
        "k8s.gcr.io/pause:3.5",
        "k8s.gcr.io/etcd:3.5.0-0",
        "k8s.gcr.io/coredns/coredns:v1.8.4"
    ]
}
`
	output := cleanImages([]byte(jsonstrImage))
	var kubeadmImage image.KubeadmImage
	if err := json.Unmarshal(output, &kubeadmImage); err != nil {
		fmt.Errorf("failed to unmarshal kubeadm version %v", err)
	}
	fmt.Println(kubeadmImage.Images)
}
func cleanImages(in []byte) []byte {
	inStr := string(in)
	// 如果 inStr 中不包含 W0508，则直接返回 in
	if !strings.Contains(inStr, "W0508") {
		return in
	}

	// 把 inStr 按照 \n 分割成一个数组
	parts := strings.Split(inStr, "\n")
	index := 0
	for _, p := range parts {
		if strings.HasPrefix(p, "W0508") {
			index += 1
		}
	}
	newInStr := strings.Join(parts[index:], "\n")
	klog.V(2).Infof("cleaned images: %+v", newInStr)

	return []byte(newInStr)
}
