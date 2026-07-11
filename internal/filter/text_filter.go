// Package filter 提供剪切板文本规范化和翻译条件判断。
package filter

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"floating-translator/internal/config"
)

// Reason 是文本被接受或跳过的原因。
type Reason string

const (
	// ReasonOK 表示文本可以翻译。
	ReasonOK Reason = "ok"
	// ReasonEmpty 表示文本为空。
	ReasonEmpty Reason = "empty_text"
	// ReasonDuplicate 表示文本与上一次观察到的内容相同。
	ReasonDuplicate Reason = "duplicate_text"
	// ReasonNotEnglish 表示文本不满足英文比例。
	ReasonNotEnglish Reason = "not_english"
	// ReasonTooLong 表示文本超过长度限制。
	ReasonTooLong Reason = "too_long"
	// ReasonURL 表示整段文本是 URL。
	ReasonURL Reason = "url"
	// ReasonCode 表示文本具有明显代码特征。
	ReasonCode Reason = "code"
	// ReasonSensitive 表示文本疑似包含敏感凭据。
	ReasonSensitive Reason = "sensitive"
)

// Result 是文本过滤结果。
type Result struct {
	ShouldTranslate bool
	Reason          Reason
	Text            string
}

// TextFilter 根据配置判断文本是否需要翻译。
type TextFilter struct {
	config config.ClipboardConfig
}

var (
	skTokenPattern      = regexp.MustCompile(`(?i)\bsk-(?:[a-z0-9_-]+)?`)
	stripeKeyPattern    = regexp.MustCompile(`\bsk_(?:live|test)_[A-Za-z0-9]*`)
	awsKeyPattern       = regexp.MustCompile(`\bAKIA[0-9A-Z]*\b`)
	githubTokenPattern  = regexp.MustCompile(`\b(?:gh[opusr]_\w*|github_pat_[A-Za-z0-9_]*)`)
	gitlabTokenPattern  = regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]*`)
	googleAPIKeyPattern = regexp.MustCompile(`\bAIza[0-9A-Za-z_-]*`)
	slackTokenPattern   = regexp.MustCompile(`\bxox[baprs]-[0-9A-Za-z-]*`)
	npmTokenPattern     = regexp.MustCompile(`\bnpm_[A-Za-z0-9]*`)
	pypiTokenPattern    = regexp.MustCompile(`\bpypi-[A-Za-z0-9_-]*`)
	jwtPattern          = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\b`)
	bearerTokenPattern  = regexp.MustCompile(`(?i)\bAuthorization\s*:\s*Bearer\s+[A-Za-z0-9._~+/=-]+`)
	privateKeyPattern   = regexp.MustCompile(`(?i)-----BEGIN(?: [A-Z0-9]+)* PRIVATE KEY-----`)
	assignmentPattern   = regexp.MustCompile(`(?i)\b(password|passphrase|token|secret|api[_-]?key|access[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret|private[_-]?key|authorization|aws[_-]?(?:access[_-]?key[_-]?id|secret[_-]?access[_-]?key))\s*[:=]\s*(?:Bearer\s+)?[^\s,;]*`)
	strongCodePattern   = regexp.MustCompile("(?im)(^\\s*```|^\\s*package\\s+main\\b|^\\s*func\\s+main\\s*\\(|^\\s*#include\\b|^\\s*public\\s+static\\s+void\\b|^\\s*import\\s*\\(|^\\s*class\\s+\\w+\\s*\\{|^\\s*(?:const|let|var)\\s+[a-z_$][\\w$]*\\s*=)")
	weakCodePatterns    = []*regexp.Regexp{
		regexp.MustCompile(`(?m)^\s*(?:func|package|import|class|interface|struct)\s+`),
		regexp.MustCompile(`=>`),
		regexp.MustCompile(`(?m)[{};]\s*$`),
		regexp.MustCompile(`(?m)^\s*(?:if|for|while|switch)\s*\([^\n]*\)\s*\{?`),
	}
)

type sensitiveRule struct {
	pattern     *regexp.Regexp
	replacement string
}

var sensitiveRules = []sensitiveRule{
	{pattern: skTokenPattern, replacement: "[REDACTED_SK_TOKEN]"},
	{pattern: stripeKeyPattern, replacement: "[REDACTED_STRIPE_KEY]"},
	{pattern: awsKeyPattern, replacement: "[REDACTED_AWS_KEY]"},
	{pattern: githubTokenPattern, replacement: "[REDACTED_GITHUB_TOKEN]"},
	{pattern: gitlabTokenPattern, replacement: "[REDACTED_GITLAB_TOKEN]"},
	{pattern: googleAPIKeyPattern, replacement: "[REDACTED_GOOGLE_API_KEY]"},
	{pattern: slackTokenPattern, replacement: "[REDACTED_SLACK_TOKEN]"},
	{pattern: npmTokenPattern, replacement: "[REDACTED_NPM_TOKEN]"},
	{pattern: pypiTokenPattern, replacement: "[REDACTED_PYPI_TOKEN]"},
	{pattern: jwtPattern, replacement: "[REDACTED_JWT]"},
	{pattern: bearerTokenPattern, replacement: "Authorization: Bearer [REDACTED_BEARER_TOKEN]"},
	{pattern: privateKeyPattern, replacement: "[REDACTED_PRIVATE_KEY]"},
	{pattern: assignmentPattern, replacement: "$1=[REDACTED]"},
}

// New 创建文本过滤器。
func New(cfg config.ClipboardConfig) *TextFilter {
	return &TextFilter{config: cfg}
}

// Evaluate 判断文本是否需要翻译。
func (f *TextFilter) Evaluate(text string) Result {
	normalized := Normalize(text)
	if normalized == "" {
		return skipped(ReasonEmpty, normalized)
	}
	if utf8.RuneCountInString(normalized) > f.config.MaxTextLength {
		return skipped(ReasonTooLong, normalized)
	}
	if f.config.SkipSensitive && ContainsSensitive(normalized) {
		return skipped(ReasonSensitive, normalized)
	}
	if f.config.SkipURL && isURL(normalized) {
		return skipped(ReasonURL, normalized)
	}
	if f.config.SkipCode && looksLikeCode(normalized) {
		return skipped(ReasonCode, normalized)
	}
	if f.config.OnlyTranslateEnglish && !isEnglish(normalized, f.config.EnglishMinRatio, f.config.ChineseMaxRatio) {
		return skipped(ReasonNotEnglish, normalized)
	}
	return Result{ShouldTranslate: true, Reason: ReasonOK, Text: normalized}
}

// Normalize 规范化首尾空白和换行，不改变正文内部空格。
func Normalize(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.TrimSpace(text)
}

// ContainsSensitive 判断文本是否包含已知凭据特征。
func ContainsSensitive(text string) bool {
	for _, rule := range sensitiveRules {
		if rule.pattern.MatchString(text) {
			return true
		}
	}
	return false
}

// RedactSensitive 对已知凭据内容执行脱敏。
func RedactSensitive(text string) string {
	for _, rule := range sensitiveRules {
		text = rule.pattern.ReplaceAllString(text, rule.replacement)
	}
	return text
}

func skipped(reason Reason, text string) Result {
	return Result{ShouldTranslate: false, Reason: reason, Text: text}
}

func isURL(text string) bool {
	candidate := text
	if strings.HasPrefix(strings.ToLower(candidate), "www.") {
		candidate = "https://" + candidate
	}
	parsed, err := url.ParseRequestURI(candidate)
	if err != nil || parsed.Host == "" {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func looksLikeCode(text string) bool {
	if strongCodePattern.MatchString(text) {
		return true
	}
	signals := 0
	for _, pattern := range weakCodePatterns {
		if pattern.MatchString(text) {
			signals++
		}
	}
	return signals >= 2
}

func isEnglish(text string, minEnglishRatio float64, maxChineseRatio float64) bool {
	latinLetters := 0
	hanLetters := 0
	allLetters := 0
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		allLetters++
		if unicode.In(r, unicode.Latin) {
			latinLetters++
		}
		if unicode.In(r, unicode.Han) {
			hanLetters++
		}
	}
	if allLetters == 0 {
		return false
	}
	englishRatio := float64(latinLetters) / float64(allLetters)
	chineseRatio := float64(hanLetters) / float64(allLetters)
	return englishRatio >= minEnglishRatio && chineseRatio <= maxChineseRatio
}
