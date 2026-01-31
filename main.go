package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type registryConfig struct {
	Registry string `json:"registry"`
	Username string `json:"username"`
	Password string `json:"password"`
	Insecure bool   `json:"insecure"`
}

type imageConfig struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

type migrateConfig struct {
	Source      registryConfig `json:"source"`
	Destination registryConfig `json:"destination"`
	Images      []imageConfig  `json:"images"`
}

type commonFlags struct {
	username string
	password string
	insecure bool
}

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "list-images":
		if err := runListImages(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "list-tags":
		if err := runListTags(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "migrate":
		if err := runMigrate(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "-h", "--help", "help":
		printUsage(os.Stdout)
	default:
		printUsage(os.Stderr)
		os.Exit(1)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "镜像管理工具 (ikl)")
	fmt.Fprintln(w, "\nUsage:")
	fmt.Fprintln(w, "  ikl list-images --registry <registry> [--username <u> --password <p> --insecure]")
	fmt.Fprintln(w, "  ikl list-tags --repository <registry/repo> [--username <u> --password <p> --insecure]")
	fmt.Fprintln(w, "  ikl migrate --config <config.json>")
}

func runListImages(args []string) error {
	fs := flag.NewFlagSet("list-images", flag.ContinueOnError)
	registry := fs.String("registry", "", "目标镜像仓库地址，例如 registry.example.com")
	flags := addCommonFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *registry == "" {
		return errors.New("必须指定 --registry")
	}

	client := newRegistryClient(registryConfig{Registry: *registry, Username: flags.username, Password: flags.password, Insecure: flags.insecure})
	catalog, err := client.listCatalog(context.Background())
	if err != nil {
		return err
	}
	for _, entry := range catalog.Repositories {
		fmt.Println(entry)
	}
	return nil
}

func runListTags(args []string) error {
	fs := flag.NewFlagSet("list-tags", flag.ContinueOnError)
	repository := fs.String("repository", "", "仓库地址，例如 registry.example.com/repo/image")
	flags := addCommonFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *repository == "" {
		return errors.New("必须指定 --repository")
	}

	registry, repo, err := splitRepository(*repository)
	if err != nil {
		return err
	}
	client := newRegistryClient(registryConfig{Registry: registry, Username: flags.username, Password: flags.password, Insecure: flags.insecure})
	tags, err := client.listTags(context.Background(), repo)
	if err != nil {
		return err
	}
	for _, tag := range tags.Tags {
		fmt.Println(tag)
	}
	return nil
}

func runMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	configPath := fs.String("config", "", "配置文件路径")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		return errors.New("必须指定 --config")
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}

	sourceClient := newRegistryClient(cfg.Source)
	destClient := newRegistryClient(cfg.Destination)

	for _, image := range cfg.Images {
		if strings.TrimSpace(image.Name) == "" {
			return errors.New("images.name 不能为空")
		}
		registryRepo := image.Name
		tags := image.Tags
		if len(tags) == 0 {
			list, err := sourceClient.listTags(context.Background(), registryRepo)
			if err != nil {
				return fmt.Errorf("获取标签失败 %s: %w", registryRepo, err)
			}
			tags = list.Tags
		}
		for _, tag := range tags {
			srcRef := fmt.Sprintf("%s/%s:%s", cfg.Source.Registry, registryRepo, tag)
			dstRef := fmt.Sprintf("%s/%s:%s", cfg.Destination.Registry, registryRepo, tag)
			fmt.Printf("复制 %s -> %s\n", srcRef, dstRef)
			if err := migrateImage(context.Background(), sourceClient, destClient, registryRepo, tag); err != nil {
				return err
			}
		}
	}
	return nil
}

type registryClient struct {
	registry string
	client   *http.Client
	username string
	password string
	baseURL  string
}

func newRegistryClient(cfg registryConfig) *registryClient {
	registry := strings.TrimSuffix(cfg.Registry, "/")
	baseURL := registry
	if !strings.HasPrefix(registry, "http://") && !strings.HasPrefix(registry, "https://") {
		baseURL = "https://" + registry
	}
	registry = strings.TrimPrefix(strings.TrimPrefix(registry, "https://"), "http://")
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.Insecure},
	}
	client := &http.Client{Transport: transport, Timeout: 60 * time.Second}
	return &registryClient{
		registry: registry,
		client:   client,
		username: cfg.Username,
		password: cfg.Password,
		baseURL:  baseURL,
	}
}

func (c *registryClient) addAuth(req *http.Request) {
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
}

type catalogResponse struct {
	Repositories []string `json:"repositories"`
}

type tagsResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

type manifestList struct {
	SchemaVersion int `json:"schemaVersion"`
	Manifests     []struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
	} `json:"manifests"`
}

type imageManifest struct {
	SchemaVersion int `json:"schemaVersion"`
	Config        struct {
		Digest string `json:"digest"`
	} `json:"config"`
	Layers []struct {
		Digest string `json:"digest"`
	} `json:"layers"`
}

func (c *registryClient) listCatalog(ctx context.Context) (*catalogResponse, error) {
	endpoint := c.baseURL + "/v2/_catalog"
	body, _, err := c.doRequest(ctx, http.MethodGet, endpoint, "", nil)
	if err != nil {
		return nil, err
	}
	var resp catalogResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *registryClient) listTags(ctx context.Context, repo string) (*tagsResponse, error) {
	endpoint := fmt.Sprintf("%s/v2/%s/tags/list", c.baseURL, repo)
	body, _, err := c.doRequest(ctx, http.MethodGet, endpoint, "", nil)
	if err != nil {
		return nil, err
	}
	var resp tagsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func migrateImage(ctx context.Context, source, destination *registryClient, repo, tag string) error {
	manifestBody, contentType, digest, err := source.getManifest(ctx, repo, tag)
	if err != nil {
		return err
	}

	if isManifestList(contentType) {
		var list manifestList
		if err := json.Unmarshal(manifestBody, &list); err != nil {
			return err
		}
		for _, entry := range list.Manifests {
			manifestBody, contentType, _, err := source.getManifestByDigest(ctx, repo, entry.Digest)
			if err != nil {
				return err
			}
			if err := copySingleManifest(ctx, source, destination, repo, entry.Digest, contentType, manifestBody); err != nil {
				return err
			}
		}
		return destination.putManifest(ctx, repo, tag, contentType, digest, manifestBody)
	}

	if err := copySingleManifest(ctx, source, destination, repo, tag, contentType, manifestBody); err != nil {
		return err
	}
	return destination.putManifest(ctx, repo, tag, contentType, digest, manifestBody)
}

func copySingleManifest(ctx context.Context, source, destination *registryClient, repo, ref, contentType string, manifestBody []byte) error {
	var manifest imageManifest
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		return err
	}
	if manifest.Config.Digest != "" {
		if err := copyBlob(ctx, source, destination, repo, manifest.Config.Digest); err != nil {
			return err
		}
	}
	for _, layer := range manifest.Layers {
		if err := copyBlob(ctx, source, destination, repo, layer.Digest); err != nil {
			return err
		}
	}
	return destination.putManifest(ctx, repo, ref, contentType, "", manifestBody)
}

func (c *registryClient) getManifest(ctx context.Context, repo, tag string) ([]byte, string, string, error) {
	endpoint := fmt.Sprintf("%s/v2/%s/manifests/%s", c.baseURL, repo, tag)
	return c.getManifestFromEndpoint(ctx, endpoint)
}

func (c *registryClient) getManifestByDigest(ctx context.Context, repo, digest string) ([]byte, string, string, error) {
	endpoint := fmt.Sprintf("%s/v2/%s/manifests/%s", c.baseURL, repo, digest)
	return c.getManifestFromEndpoint(ctx, endpoint)
}

func (c *registryClient) getManifestFromEndpoint(ctx context.Context, endpoint string) ([]byte, string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.docker.distribution.manifest.v1+json",
	}, ", "))
	c.addAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", "", fmt.Errorf("获取 manifest 失败: %s", strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", "", err
	}
	return body, resp.Header.Get("Content-Type"), resp.Header.Get("Docker-Content-Digest"), nil
}

func (c *registryClient) putManifest(ctx context.Context, repo, ref, contentType, digest string, body []byte) error {
	endpoint := fmt.Sprintf("%s/v2/%s/manifests/%s", c.baseURL, repo, ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if digest != "" {
		req.Header.Set("Docker-Content-Digest", digest)
	}
	c.addAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("推送 manifest 失败: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func copyBlob(ctx context.Context, source, destination *registryClient, repo, digest string) error {
	exists, err := destination.blobExists(ctx, repo, digest)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	blob, err := source.getBlob(ctx, repo, digest)
	if err != nil {
		return err
	}
	defer blob.Close()
	return destination.uploadBlob(ctx, repo, digest, blob)
}

func (c *registryClient) blobExists(ctx context.Context, repo, digest string) (bool, error) {
	endpoint := fmt.Sprintf("%s/v2/%s/blobs/%s", c.baseURL, repo, digest)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
	if err != nil {
		return false, err
	}
	c.addAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	body, _ := io.ReadAll(resp.Body)
	return false, fmt.Errorf("检查 blob 失败: %s", strings.TrimSpace(string(body)))
}

func (c *registryClient) getBlob(ctx context.Context, repo, digest string) (io.ReadCloser, error) {
	endpoint := fmt.Sprintf("%s/v2/%s/blobs/%s", c.baseURL, repo, digest)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	c.addAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("下载 blob 失败: %s", strings.TrimSpace(string(body)))
	}
	return resp.Body, nil
}

func (c *registryClient) uploadBlob(ctx context.Context, repo, digest string, reader io.Reader) error {
	start := fmt.Sprintf("%s/v2/%s/blobs/uploads/", c.baseURL, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, start, nil)
	if err != nil {
		return err
	}
	c.addAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("创建上传失败: %s", resp.Status)
	}
	location := resp.Header.Get("Location")
	if location == "" {
		return errors.New("缺少上传地址")
	}
	uploadURL, err := resolveLocation(c.baseURL, location)
	if err != nil {
		return err
	}
	uploadURL = uploadURL + "?digest=" + url.QueryEscape(digest)
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, reader)
	if err != nil {
		return err
	}
	c.addAuth(putReq)
	putResp, err := c.client.Do(putReq)
	if err != nil {
		return err
	}
	defer putResp.Body.Close()
	if putResp.StatusCode >= 300 {
		body, _ := io.ReadAll(putResp.Body)
		return fmt.Errorf("上传 blob 失败: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *registryClient) doRequest(ctx context.Context, method, endpoint, contentType string, body io.Reader) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	c.addAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("请求失败: %s", strings.TrimSpace(string(bodyBytes)))
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	return bodyBytes, resp.Header, nil
}

func resolveLocation(base, location string) (string, error) {
	if strings.HasPrefix(location, "http://") || strings.HasPrefix(location, "https://") {
		return location, nil
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(location)
	if err != nil {
		return "", err
	}
	return baseURL.ResolveReference(ref).String(), nil
}

func addCommonFlags(fs *flag.FlagSet) *commonFlags {
	flags := &commonFlags{}
	fs.StringVar(&flags.username, "username", "", "用户名")
	fs.StringVar(&flags.password, "password", "", "密码")
	fs.BoolVar(&flags.insecure, "insecure", false, "跳过TLS校验")
	return flags
}

func loadConfig(path string) (*migrateConfig, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	var cfg migrateConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Source.Registry == "" || cfg.Destination.Registry == "" {
		return nil, errors.New("source.registry 和 destination.registry 不能为空")
	}
	return &cfg, nil
}

func splitRepository(full string) (string, string, error) {
	parts := strings.SplitN(full, "/", 2)
	if len(parts) != 2 {
		return "", "", errors.New("repository 必须包含 registry/镜像名称")
	}
	return parts[0], parts[1], nil
}

func isManifestList(contentType string) bool {
	return strings.Contains(contentType, "manifest.list") || strings.Contains(contentType, "image.index")
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "错误: %v\n", err)
	os.Exit(1)
}
