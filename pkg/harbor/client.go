package harbor

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL  string
	Username string
	Password string
	Client   *http.Client
}

// NewClient 创建 Harbor API 客户端
// address: 例如 "jusuan.io:8080"
func NewClient(address, username, password string, insecure bool, proxyURL string, noProxy string) (*Client, error) {
	// 默认使用 HTTPS，除非用户在地址中明确指定了 http://
	baseURL := address
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
	}

	// 处理代理
	if proxyURL != "" {
		pURL, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("无效代理地址: %w", err)
		}

		noProxyList := strings.Split(noProxy, ",")
		for i := range noProxyList {
			noProxyList[i] = strings.TrimSpace(noProxyList[i])
		}

		transport.Proxy = func(req *http.Request) (*url.URL, error) {
			host := req.URL.Hostname()
			for _, np := range noProxyList {
				if np == "" {
					continue
				}
				if host == np || strings.HasSuffix(host, "."+np) {
					return nil, nil // 直连
				}
			}
			return pURL, nil
		}
	}

	return &Client{
		BaseURL:  baseURL,
		Username: username,
		Password: password,
		Client: &http.Client{
			Transport: transport,
			Timeout:   10 * time.Second,
		},
	}, nil
}

// EnsureProject 检查项目是否存在，不存在则创建
func (c *Client) EnsureProject(project string) error {
	exists, err := c.checkProjectExists(project)

	// 自动协议降级逻辑：
	// 如果配置了 HTTPS 但服务端是 HTTP，Go 会报 "http: server gave HTTP response to HTTPS client"
	if err != nil && strings.Contains(err.Error(), "server gave HTTP response to HTTPS client") {
		if strings.HasPrefix(c.BaseURL, "https://") {
			newURL := strings.Replace(c.BaseURL, "https://", "http://", 1)
			fmt.Printf("🔄 [Harbor] 检测到服务端返回 HTTP，自动降级协议重试 (%s -> %s)...\n", c.BaseURL, newURL)

			// 更新客户端的 BaseURL，后续 createProject 也会使用这个新地址
			c.BaseURL = newURL

			// 使用 HTTP 重试检查
			exists, err = c.checkProjectExists(project)
		}
	}

	if err != nil {
		return fmt.Errorf("检查项目 %s 失败: %w", project, err)
	}

	if exists {
		return nil
	}

	fmt.Printf("✨ 目标 Harbor 项目 '%s' 不存在，正在自动创建...\n", project)
	return c.createProject(project)
}

func (c *Client) checkProjectExists(project string) (bool, error) {
	// Harbor V2 API: GET /api/v2.0/projects?name=xxx
	apiURL := fmt.Sprintf("%s/api/v2.0/projects?name=%s", c.BaseURL, project)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return false, err
	}
	req.SetBasicAuth(c.Username, c.Password)

	resp, err := c.Client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return false, fmt.Errorf("认证失败 (401) - 请检查 Harbor 账号密码")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("API 响应错误: %d, Body: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	var projects []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &projects); err != nil {
		return false, fmt.Errorf("解析响应失败: %w", err)
	}

	for _, p := range projects {
		if p.Name == project {
			return true, nil
		}
	}

	return false, nil
}

func (c *Client) createProject(project string) error {
	apiURL := fmt.Sprintf("%s/api/v2.0/projects", c.BaseURL)

	payload := map[string]interface{}{
		"project_name": project,
		"metadata": map[string]string{
			"public": "false", // 默认创建为私有项目
		},
	}
	jsonBody, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.Username, c.Password)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		return nil
	} else if resp.StatusCode == http.StatusConflict {
		// 并发或刚创建，视为成功
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("创建失败 (%d): %s", resp.StatusCode, string(body))
}
