package image

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/caoyingjunz/pixiulib/exec"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"k8s.io/klog/v2"
)

const (
	Kubeadm   = "kubeadm"
	IgnoreKey = "W0508"

	User     = "user"     // 修改成实际的 docker 用户名
	Password = "password" // 修改为实际的 docker 密码
)

type KubeadmVersion struct {
	ClientVersion struct {
		GitVersion string `json:"gitVersion"`
	} `json:"clientVersion"`
}

type KubeadmImage struct {
	Images []string `json:"images"`
}

type Image struct {
	KubernetesVersion string
	ImageRepository   string

	User     string
	Password string

	exec exec.Interface
	// docker 客户端
	docker *client.Client

	Cfg Config
}

// Validate
// ①. 如果配置了 push kubernetes 为 true
//
//	Ⅰ. 则需要检查是否正常获取到 k8s 的版本（ 命令行/配置文件/环境变量中至少要有一个写了版本 ）
//	Ⅱ. 然后检查 kubeadm 的版本是否可以正常获取
//	Ⅲ. 再检查 kubeadm 和我们在 命令行/配置文件/环境变量中指定的 k8s 版本一致
//
// ②. 检查 docker 客户端连接是否正常
func (img *Image) Validate() error {
	// 如果配置了 push kubernetes 为 true ，才去校验以下步骤
	if img.Cfg.Default.PushKubernetes {
		// 检查是否正常获取到 k8s 的版本（ 命令行/配置文件/环境变量中至少要有一个写了版本 ）
		if len(img.KubernetesVersion) == 0 {
			return fmt.Errorf("failed to find kubernetes version")
		}
		// 检查 kubeadm 的版本是否和我们在 命令行/配置文件/环境变量中指定的 k8s 版本一致
		kubeadmVersion, err := img.getKubeadmVersion()
		if err != nil {
			return fmt.Errorf("failed to get kubeadm version: %v", err)
		}
		if kubeadmVersion != img.KubernetesVersion {
			return fmt.Errorf("kubeadm version %s not match kubernetes version %s", kubeadmVersion, img.KubernetesVersion)
		}
	}

	// 检查 docker 的客户端是否正常
	if _, err := img.docker.Ping(context.Background()); err != nil {
		return err
	}

	return nil
}

// Complete 创建 docker 客户端 , 赋值给 Image 结构体中的 docker 字段，也就是 img.docker
// ①. 如果配置了 push kubernetes 为 true ，
// 则需要从 命令行/配置文件/环境变量 获取 k8s 的版本，赋值给 img.KubernetesVersion
// 并且安装 kubeadm（对应版本）
// ②. 如果配置了 push kubernetes 为 false，
// 则不用获取 k8s 的版本，不用赋值 img.KubernetesVersion，
// 也不用安装 kubeadm
func (img *Image) Complete() error {
	// 创建 docker 客户端，需要先安装 docker，并且环境变量中有 DOCKER_HOST，DOCKER_API_VERSION，DOCKER_CERT_PATH，DOCKER_TLS_VERIFY
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	// 把 docker 客户端赋值给 Image 结构体中的 docker 字段，绑定到 Image 结构体中
	img.docker = cli

	// 如果配置了 push kubernetes 为 true ，则需要获取 k8s 的版本
	// 如果为 false 则不用获取 k8s 的版本
	if img.Cfg.Default.PushKubernetes {
		// 如果命令行中没有指定 k8s 的版本，则从配置文件中获取，命令行有指定，则跳过 85 - 95 行，从命令行中获取
		if len(img.KubernetesVersion) == 0 {
			// 如果配置文件中指定了 k8s 的版本，则从配置文件中获取
			if len(img.Cfg.Kubernetes.Version) != 0 {
				img.KubernetesVersion = img.Cfg.Kubernetes.Version
				//	如果配置文件中没有指定 k8s 的版本，则从环境变量中获取
			} else {
				img.KubernetesVersion = os.Getenv("KubernetesVersion")
			}
		}
	}

	// 没在命令行中指定 docker 用户名，则从该文件的 const 中获取
	if len(img.User) == 0 {
		img.User = User
	}
	// 没在命令行中指定 docker 密码，则从该文件的 const 中获取
	if len(img.Password) == 0 {
		img.Password = Password
	}

	// 创建 exec 客户端
	img.exec = exec.New()

	// 如果配置文件中指定了 push kubernetes 为 true，则需要安装 kubeadm（对应版本）
	if img.Cfg.Default.PushKubernetes {
		// 拼接命令，安装 kubeadm，img.Cfg.Kubernetes.Version[1:] 是去掉第一个字母 v
		cmd := []string{"sudo", "apt-get", "install", "-y", fmt.Sprintf("kubeadm=%s-00", img.Cfg.Kubernetes.Version[1:])} // [sudo apt-get install -y kubeadm=1.23.6-00]
		// 如果有错误，则返回错误
		out, err := img.exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to install kubeadm %v %v", string(out), err)
		}
	}
	return nil
}

// Close 关闭 docker 客户端
func (img *Image) Close() {
	if img.docker != nil {
		_ = img.docker.Close()
	}
}

// 获取 kubeadm 的版本，这里的 kubeadm 是通过 apt-get 安装的，所以可以直接通过 kubeadm version 获取版本
// 根据  kubeadm version  -o json 获取其中的 gitVersion 版本，返回值类似于 v1.23.6
func (img *Image) getKubeadmVersion() (string, error) {
	// 确认命令行是否存在
	if _, err := img.exec.LookPath(Kubeadm); err != nil {
		return "", fmt.Errorf("failed to find %s %v", Kubeadm, err)
	}
	// 拼接命令，获取 kubeadm 的版本
	cmd := []string{Kubeadm, "version", "-o", "json"}
	// out 是命令执行的结果，err 是命令执行的错误，out 是 []byte 类型
	out, err := img.exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to exec kubeadm version %v %v", string(out), err)
	}

	// 把 kubeadm 的版本信息赋值给 kubeadmVersion
	var kubeadmVersion KubeadmVersion
	// 把 out 中的内容赋值给 kubeadmVersion，
	if err := json.Unmarshal(out, &kubeadmVersion); err != nil {
		return "", fmt.Errorf("failed to unmarshal kubeadm version %v", err)
	}
	klog.V(2).Infof("kubeadmVersion %+v", kubeadmVersion)

	return kubeadmVersion.ClientVersion.GitVersion, nil
}

func (img *Image) cleanImages(in []byte) []byte {
	inStr := string(in)
	if !strings.Contains(inStr, IgnoreKey) {
		return in
	}

	klog.V(2).Infof("cleaning images: %+v", inStr)
	parts := strings.Split(inStr, "\n")
	index := 0
	for _, p := range parts {
		if strings.HasPrefix(p, IgnoreKey) {
			index += 1
		}
	}
	newInStr := strings.Join(parts[index:], "\n")
	klog.V(2).Infof("cleaned images: %+v", newInStr)

	return []byte(newInStr)
}

// getImages 根据 kubeadm 命令获取所需要的 k8s 镜像列表
func (img *Image) getImages() ([]string, error) {
	cmd := []string{Kubeadm, "config", "images", "list", "--kubernetes-version", img.KubernetesVersion, "-o", "json"}
	out, err := img.exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
	// [root@localhost ~]# kubeadm config images list --kubernetes-version 1.22.8 -o json
	// {
	//    "kind": "Images",
	//    "apiVersion": "output.kubeadm.k8s.io/v1alpha1",
	//    "images": [
	//        "k8s.gcr.io/kube-apiserver:v1.22.8",
	//        "k8s.gcr.io/kube-controller-manager:v1.22.8",
	//        "k8s.gcr.io/kube-scheduler:v1.22.8",
	//        "k8s.gcr.io/kube-proxy:v1.22.8",
	//        "k8s.gcr.io/pause:3.5",
	//        "k8s.gcr.io/etcd:3.5.0-0",
	//        "k8s.gcr.io/coredns/coredns:v1.8.4"
	//    ]
	// }
	if err != nil {
		return nil, fmt.Errorf("failed to exec kubeadm config images list %v %v", string(out), err)
	}
	out = img.cleanImages(out)
	klog.V(2).Infof("images is %+v", string(out))

	var kubeadmImage KubeadmImage
	if err := json.Unmarshal(out, &kubeadmImage); err != nil {
		return nil, fmt.Errorf("failed to unmarshal kubeadm images %v", err)
	}

	return kubeadmImage.Images, nil
}

// parseTargetImage 传入的是镜像列表中的每一个镜像，
// 比如 k8s.gcr.io/coredns/coredns:v1.8.4
// 比如 docker.io/nginx:latest
// 解析出来的是 docker 仓库中的镜像，比如 pixiuio/coredns:v1.8.4
func (img *Image) parseTargetImage(imageToPush string) (string, error) {
	// real image to push
	parts := strings.Split(imageToPush, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invaild image format: %s", imageToPush)
	}

	return img.ImageRepository + "/" + parts[len(parts)-1], nil
}

// doPushImage 传入的是镜像列表中的每一个镜像，
// 比如 k8s.gcr.io/coredns/coredns:v1.8.4
// 比如 docker.io/nginx:latest
func (img *Image) doPushImage(imageToPush string) error {
	targetImage, err := img.parseTargetImage(imageToPush)
	if err != nil {
		return err
	}

	klog.Infof("starting pull image %s", imageToPush)
	// 拉取镜像,比如拉取 k8s.gcr.io/coredns/coredns:v1.8.4
	reader, err := img.docker.ImagePull(context.TODO(), imageToPush, types.ImagePullOptions{})
	if err != nil {
		klog.Errorf("failed to pull %s: %v", imageToPush, err)
		return err
	}
	// io.Copy(dst, src) 从 src 中读取内容，写入到 dst 中
	io.Copy(os.Stdout, reader)

	// 重新给镜像打 tag, 比如 k8s.gcr.io/coredns/coredns:v1.8.4 打 tag 为 pixiuio/coredns:v1.8.4
	klog.Infof("tag %s to %s", imageToPush, targetImage)
	if err := img.docker.ImageTag(context.TODO(), imageToPush, targetImage); err != nil {
		klog.Errorf("failed to tag %s to %s: %v", imageToPush, targetImage, err)
		return err
	}

	// 开始推送镜像到 docker 仓库
	klog.Infof("starting push image %s", targetImage)
	cmd := []string{"docker", "push", targetImage}
	out, err := img.exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to push image %s %v %v", targetImage, string(out), err)
	}

	klog.Infof("complete push image %s", imageToPush)
	return nil
}

// getImagesFromFile
// 获取配置文件中的镜像列表,也就是 config 中 images 的列表
// 从配置文件中获取的镜像列表，需要 push 到 docker 仓库, 把这个镜像列表赋值给 imgs , 返回值也是 imgs
func (img *Image) getImagesFromFile() ([]string, error) {
	var imgs []string
	for _, i := range img.Cfg.Images {
		imageStr := strings.TrimSpace(i)
		if len(imageStr) == 0 {
			continue
		}
		if strings.Contains(imageStr, " ") {
			return nil, fmt.Errorf("error image format: %s", imageStr)
		}

		imgs = append(imgs, imageStr)
	}

	return imgs, nil
}

// PushImages 并发推送镜像到 docker 仓库
func (img *Image) PushImages() error {
	var images []string
	// 如果配置了 push kubernetes 为 true ，则需要获取 k8s 的镜像列表
	if img.Cfg.Default.PushKubernetes {
		// 获取 k8s 的镜像列表
		kubeImages, err := img.getImages()
		if err != nil {
			return fmt.Errorf("获取 k8s 镜像失败: %v", err)
		}
		images = append(images, kubeImages...)
	}
	// 如果配置了 push images 为 true ，则需要获取配置文件中的镜像列表
	if img.Cfg.Default.PushImages {
		fileImages, err := img.getImagesFromFile()
		if err != nil {
			return fmt.Errorf("")
		}
		images = append(images, fileImages...)
	}

	klog.V(2).Infof("get images: %v", images)
	// 拿到所需要上传的镜像数量 , 也就是镜像列表的长度
	diff := len(images)
	// 创建一个 error 类型的 channel，长度为 diff
	errCh := make(chan error, diff)

	// 登陆
	cmd := []string{"docker", "login", "-u", img.User, "-p", img.Password}
	out, err := img.exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to login in image %v %v", string(out), err)
	}
	// 一共有 diff 个数量的镜像, 开启 diff 个 goroutine
	var wg sync.WaitGroup
	wg.Add(diff)
	for _, i := range images {
		go func(imageToPush string) {
			defer wg.Done()
			if err := img.doPushImage(imageToPush); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()

	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	default:
	}

	return nil
}
