package test_manage

import (
	"encoding/json"
	"strings"
	"testing"
)

// Cookie 最小结构，匹配 go-rod proto.NetworkCookie 的关键字段
type Cookie struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Value  string `json:"value"`
}

// isCreatorDomain 判断是否为 creator 域名
func isCreatorDomain(domain string) bool {
	return strings.Contains(domain, "creator.xiaohongshu.com")
}

// filterOutMainCookies 移除主站 cookies，保留 creator cookies
func filterOutMainCookies(data []byte) ([]byte, error) {
	var cookies []Cookie
	if err := json.Unmarshal(data, &cookies); err != nil {
		return nil, err
	}
	var result []Cookie
	for _, c := range cookies {
		if isCreatorDomain(c.Domain) {
			result = append(result, c)
		}
	}
	return json.Marshal(result)
}

// filterOutCreatorCookies 移除 creator cookies，保留主站 cookies
func filterOutCreatorCookies(data []byte) ([]byte, error) {
	var cookies []Cookie
	if err := json.Unmarshal(data, &cookies); err != nil {
		return nil, err
	}
	var result []Cookie
	for _, c := range cookies {
		if !isCreatorDomain(c.Domain) {
			result = append(result, c)
		}
	}
	return json.Marshal(result)
}

func TestFilterOutMainCookies(t *testing.T) {
	cookies := []Cookie{
		{Name: "a1", Domain: ".xiaohongshu.com", Value: "v1"},
		{Name: "a2", Domain: "www.xiaohongshu.com", Value: "v2"},
		{Name: "c1", Domain: "creator.xiaohongshu.com", Value: "v3"},
		{Name: "c2", Domain: ".creator.xiaohongshu.com", Value: "v4"},
	}
	data, _ := json.Marshal(cookies)

	result, err := filterOutMainCookies(data)
	if err != nil {
		t.Fatalf("filterOutMainCookies 失败: %v", err)
	}

	var remaining []Cookie
	if err := json.Unmarshal(result, &remaining); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}

	if len(remaining) != 2 {
		t.Fatalf("期望保留 2 个 creator cookies，实际 %d 个: %+v", len(remaining), remaining)
	}

	for _, c := range remaining {
		if !isCreatorDomain(c.Domain) {
			t.Errorf("不应保留主站 cookie: %+v", c)
		}
	}
	t.Logf("filterOutMainCookies 保留: %+v", remaining)
}

func TestFilterOutCreatorCookies(t *testing.T) {
	cookies := []Cookie{
		{Name: "a1", Domain: ".xiaohongshu.com", Value: "v1"},
		{Name: "a2", Domain: "www.xiaohongshu.com", Value: "v2"},
		{Name: "c1", Domain: "creator.xiaohongshu.com", Value: "v3"},
		{Name: "c2", Domain: ".creator.xiaohongshu.com", Value: "v4"},
	}
	data, _ := json.Marshal(cookies)

	result, err := filterOutCreatorCookies(data)
	if err != nil {
		t.Fatalf("filterOutCreatorCookies 失败: %v", err)
	}

	var remaining []Cookie
	if err := json.Unmarshal(result, &remaining); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}

	if len(remaining) != 2 {
		t.Fatalf("期望保留 2 个主站 cookies，实际 %d 个: %+v", len(remaining), remaining)
	}

	for _, c := range remaining {
		if isCreatorDomain(c.Domain) {
			t.Errorf("不应保留 creator cookie: %+v", c)
		}
	}
	t.Logf("filterOutCreatorCookies 保留: %+v", remaining)
}

// TestBothFiltersResultInEmpty 验证先移除主站再移除 creator 后结果为空
func TestBothFiltersResultInEmpty(t *testing.T) {
	cookies := []Cookie{
		{Name: "a1", Domain: ".xiaohongshu.com", Value: "v1"},
		{Name: "c1", Domain: "creator.xiaohongshu.com", Value: "v2"},
	}
	data, _ := json.Marshal(cookies)

	// 先移除主站 cookies -> 只剩 creator
	data, err := filterOutMainCookies(data)
	if err != nil {
		t.Fatalf("filterOutMainCookies 失败: %v", err)
	}

	// 再移除 creator cookies -> 应该为空
	data, err = filterOutCreatorCookies(data)
	if err != nil {
		t.Fatalf("filterOutCreatorCookies 失败: %v", err)
	}

	var remaining []Cookie
	if err := json.Unmarshal(data, &remaining); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}

	if len(remaining) != 0 {
		t.Fatalf("期望 0 个 cookies，实际 %d 个: %+v", len(remaining), remaining)
	}
	t.Log("两次过滤后结果为空，符合预期")
}
