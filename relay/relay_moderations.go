package relay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// ---- 类别注释映射（可前端/后台辅助显示） ----
var ModerationCategoryMap = map[string]string{
	"harassment":             "骚扰言论。表达、煽动或鼓励对任何对象的骚扰性言辞。",
	"harassment/threatening": "威胁性骚扰。具有对任一对象实施暴力或重大伤害内容的骚扰。",
	"hate":                   "仇恨言论。基于种族、性别、族裔、宗教、国籍、性取向、残疾状况或种姓表达、煽动或鼓动仇恨的内容。针对非受保护群体的仇恨归为骚扰。",
	"hate/threatening":       "威胁性仇恨。除仇恨属性外，还包含对目标群体实施暴力或重大伤害的内容。",
	"illicit":                "非法行为指导。包含协助或指导违法行为的内容，如“如何偷窃”等。",
	"illicit/violent":        "暴力违法指导。包括协助或指导暴力犯罪、武器获取等违法行为。",
	"self-harm":              "自残倾向。鼓励、宣扬或描述自残行为，如自杀、割伤、进食障碍等。",
	"self-harm/instructions": "自残实施指导。具体教唆或指导如何实施自残（如方法、步骤等）。",
	"self-harm/intent":       "自残意图表露。表述自己正在、打算自残或有相关意向。",
	"sexual":                 "性内容。为了引发性兴奋的内容（描述性行为、推荐性服务，不包含性教育/健康）。",
	"sexual/minors":          "未成年人涉性。包含未满18岁个体的任何性内容。",
	"violence":               "暴力内容。描述死亡、暴力或人身伤害的内容。",
	"violence/graphic":       "血腥暴力内容。以细节展示死亡、暴力或人身伤害画面的内容。",
}

type ModerationResponse struct {
	Results []struct {
		Categories     map[string]bool    `json:"categories"`
		CategoryScores map[string]float64 `json:"category_scores"`
		Flagged        bool               `json:"flagged"`
	} `json:"results"`
}

// DoOpenAIModerationAuditing 内容审查主方法 - 基于阈值的版本
func DoOpenAIModerationAuditing(
	content string, // 需要审查的文本内容
	auditCategories []string, // 需要检查的审查类别与阈值，如[]string{"hate:0.9", "violence:0.8"}
	auditUrl string, // 审查API URL，如"https://api.openai.com/v1/moderations"
	auditApiKey string, // 审查API密钥
	auditModel string, // 审查模型（可选，若空用默认模型）
) ([]string, error) {
	if strings.TrimSpace(auditUrl) == "" {
		auditUrl = "https://api.openai.com/v1/moderations"
	}

	if strings.TrimSpace(auditModel) == "" {
		auditModel = "omni-moderation-latest"
	}
	if auditApiKey == "" {
		return nil, errors.New("API key不能为空")
	}

	// 构建请求体
	body := map[string]interface{}{
		"input": content,
	}
	if auditModel != "" {
		body["model"] = auditModel
	}

	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", auditUrl, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+auditApiKey)
	req.Header.Set("Content-Type", "application/json")

	// 发起请求
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bs, _ := io.ReadAll(resp.Body)
	fmt.Println("OpenAI Moderation API 原始响应：", string(bs))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("OpenAI Moderation接口异常: %s", bs)
	}

	// 解析响应
	var modResp ModerationResponse
	if err := json.Unmarshal(bs, &modResp); err != nil {
		return nil, err
	}
	if len(modResp.Results) == 0 {
		return nil, errors.New("内容审查无返回结果")
	}

	// 解析审查类别和阈值
	categoryThresholds := make(map[string]float64)

	for _, cat := range auditCategories {
		cat = strings.TrimSpace(cat)
		if cat == "" {
			continue
		}

		parts := strings.Split(cat, ":")
		if len(parts) == 1 {
			// 未指定阈值，跳过此类别
			continue
		} else if len(parts) == 2 {
			// 已指定阈值
			threshold, err := strconv.ParseFloat(parts[1], 64)
			if err != nil {
				// 解析阈值失败，跳过此类别
				continue
			} else {
				categoryThresholds[parts[0]] = threshold
			}
		}
	}

	// 收集达到阈值的违规项
	var violated []string
	categoryScores := modResp.Results[0].CategoryScores

	// 如果没有配置任何有效的审核类别+阈值，直接返回空数组表示通过
	if len(categoryThresholds) == 0 {
		return violated, nil
	}

	// 检查每个配置的类别是否超过阈值
	for cat, threshold := range categoryThresholds {
		if score, exists := categoryScores[cat]; exists && score >= threshold {
			desc := ModerationCategoryMap[cat]
			if desc == "" {
				desc = cat // 如果没有找到描述，就使用类别名称
			}
			violated = append(violated, fmt.Sprintf("%s (分数: %.4f, 阈值: %.4f)", desc, score, threshold))
		}
	}

	return violated, nil
}
