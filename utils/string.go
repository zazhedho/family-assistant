package utils

import (
	"html"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var (
	htmlScript = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	htmlStyle  = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	htmlTag    = regexp.MustCompile(`<[^>]*>`)
)

func StripHTML(s string) string {
	s = htmlScript.ReplaceAllString(s, " ")
	s = htmlStyle.ReplaceAllString(s, " ")
	s = htmlTag.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}

func TitleCase(s string) string {
	titleCaser := cases.Title(language.English)
	return titleCaser.String(s)
}

func StringPtrIfNotEmpty(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func Int64PtrFromString(value string) *int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func StringOrDefault(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func NormalizeKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func NormalizeUpperKey(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func FirstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func EnvKey(value string) string {
	value = NormalizeUpperKey(value)
	replacer := strings.NewReplacer("-", "_", ".", "_", " ", "_")
	return replacer.Replace(value)
}

func HumanizeKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}

	value = strings.ReplaceAll(value, "_", " ")
	value = strings.ReplaceAll(value, "-", " ")
	return strings.Join(strings.Fields(value), " ")
}

func TitleHumanized(value string) string {
	value = HumanizeKey(value)
	if value == "" {
		return value
	}

	words := strings.Fields(value)
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
	}
	return strings.Join(words, " ")
}
