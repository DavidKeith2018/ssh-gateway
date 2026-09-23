// Package localization 在应用输出边界添加独立于语言的消息元数据。
// 保留原始错误字符串，兼容现有接口调用方和日志。
package localization

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

//go:embed catalog.json
var catalogJSON []byte

type entry struct {
	Source  string `json:"source"`
	Chinese string `json:"zh-CN"`
	English string `json:"en"`
	Prefix  bool   `json:"prefix"`
	key     string
	pattern *regexp.Regexp
	verbs   []string
}

type Message struct {
	Key    string `json:"message_key,omitempty"`
	Params []any  `json:"message_params,omitempty"`
}

var formats = regexp.MustCompile(`%[+\-# .0-9]*[sqwdvf]`)
var catalog map[string]entry
var exact = map[string]entry{}
var dynamic []entry

func init() {
	if err := json.Unmarshal(catalogJSON, &catalog); err != nil {
		panic(err)
	}
	for key, item := range catalog {
		item.key = key
		positions := formats.FindAllStringIndex(item.Source, -1)
		if len(positions) == 0 && !item.Prefix {
			exact[item.Source] = item
			continue
		}
		var expression strings.Builder
		expression.WriteString("(?s)^")
		last := 0
		for _, pos := range positions {
			expression.WriteString(regexp.QuoteMeta(item.Source[last:pos[0]]))
			expression.WriteString("(.*?)")
			item.verbs = append(item.verbs, item.Source[pos[0]:pos[1]])
			last = pos[1]
		}
		expression.WriteString(regexp.QuoteMeta(item.Source[last:]))
		if item.Prefix {
			expression.WriteString("(.*)")
			verb := "%s"
			if strings.HasSuffix(item.Source, "：") {
				verb = "%v"
			}
			item.verbs = append(item.verbs, verb)
		}
		expression.WriteString("$")
		item.pattern = regexp.MustCompile(expression.String())
		dynamic = append(dynamic, item)
	}
	// 优先匹配具体错误模板，再匹配“操作失败：”等通用前缀。
	sort.Slice(dynamic, func(i, j int) bool { return len(dynamic[i].Source) > len(dynamic[j].Source) })
}

// Describe 仅用于应用错误和状态，不得用于终端输出、用户名、文件内容或历史日志。
func Describe(text string) Message { return describe(text, 0) }
func describe(text string, depth int) Message {
	if text == "context canceled" {
		return Message{Key: "backend.canceled"}
	}
	if item, ok := exact[text]; ok {
		return Message{Key: item.key}
	}
	if depth >= 8 {
		return Message{}
	}
	for _, item := range dynamic {
		matches := item.pattern.FindStringSubmatch(text)
		if matches == nil {
			continue
		}
		params := make([]any, len(matches)-1)
		for i, value := range matches[1:] {
			params[i] = value
			if strings.HasSuffix(item.verbs[i], "w") || strings.HasSuffix(item.verbs[i], "v") {
				if nested := describe(value, depth+1); nested.Key != "" {
					params[i] = nested
				}
			}
		}
		return Message{Key: item.key, Params: params}
	}
	return Message{}
}

func Normalize(value string) string {
	if value == "zh-CN" {
		return value
	}
	return "en"
}

func Text(language, source string) string {
	if Normalize(language) == "zh-CN" {
		return source
	}
	if item, ok := exact[source]; ok {
		return item.English
	}
	return source
}

func MarshalError(err error) []byte {
	if err == nil {
		return nil
	}
	result, _ := json.Marshal(struct {
		Error string `json:"error"`
		Message
	}{err.Error(), Describe(err.Error())})
	return result
}
