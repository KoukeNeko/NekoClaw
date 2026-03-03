package auth

import "regexp"

var anthropicSetupTokenPattern = regexp.MustCompile(`sk-ant-oat01-[A-Za-z0-9._-]{20,}`)

func extractAnthropicSetupTokens(input string) []string {
	return anthropicSetupTokenPattern.FindAllString(input, -1)
}

func replaceAnthropicTokenString(input string, replacer func(token string) string) string {
	if input == "" || replacer == nil {
		return input
	}
	return anthropicSetupTokenPattern.ReplaceAllStringFunc(input, replacer)
}

func safeTokenSuffix(token string) string {
	if len(token) <= 6 {
		return token
	}
	return token[len(token)-6:]
}
