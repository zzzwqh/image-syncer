package main

import (
	"flag"

	"github.com/caoyingjunz/pixiulib/config"
	"k8s.io/klog/v2"

	"go-learning/practise/image-practise/image"
)

var (
	kubernetesVersion = flag.String("kubernetes-version", "", "Choose a specific Kubernetes version for the control plane")
	imageRepository   = flag.String("image-repository", "pixiuio", "Choose a container registry to push (default pixiuio")

	user     = flag.String("user", "", "docker register user")
	password = flag.String("password", "", "docker register password")

	filePath = flag.String("file-path", "", "image file path")
)

func main() {
	// klog InitFlags 会初始化 flag
	klog.InitFlags(nil)
	flag.Parse()

	// 读取配置文件，将配置文件中的内容绑定到 Config 结构体中，这里的 Config 结构体是自定义的
	c := config.New()
	c.SetConfigFile("./config.yaml")
	c.SetConfigType("yaml")

	// option.go 中的 Config 结构体
	var cfg image.Config
	// 把 config.yaml 中的内容绑定到 cfg 中，cfg 的类型是 option.go 中的 Config 结构体
	if err := c.Binding(&cfg); err != nil {
		klog.Fatal(err)
	}

	// image.go 中的 Image 结构体，这里的 Image 结构体是自定义的，不是 docker 中的 Image 结构体，这里的 Image 结构体中有一个 Config 结构体
	img := image.Image{
		ImageRepository:   *imageRepository,
		KubernetesVersion: *kubernetesVersion,
		User:              *user,
		Password:          *password,
		Cfg:               cfg,
	}

	// 用于创建 docker 客户端
	if err := img.Complete(); err != nil {
		klog.Fatal(err)
	}
	// 延迟关闭 docker 客户端
	defer img.Close()

	// 先检查 kubeadm 的版本是否和 k8s 版本一致
	// 再检查 docker 客户端连接是否正常
	if err := img.Validate(); err != nil {
		klog.Fatal(err)
	}

	if err := img.PushImages(); err != nil {
		klog.Fatal(err)
	}
}
