package auth

import "regexp"

var openAICodexOAuthTokenPattern = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)
var openAITokenLikePattern = regexp.MustCompile(`sk-[A-Za-z0-9._-]{16,}`)

func extractOpenAICodexTokens(input string) []string {
	matches := openAICodexOAuthTokenPattern.FindAllString(input, -1)
	if len(matches) > 0 {
		return matches
	}
	return openAITokenLikePattern.FindAllString(input, -1)
}

func replaceOpenAICodexTokenString(input string, replacer func(token string) string) string {
	if input == "" || replacer == nil {
		return input
	}
	output := openAICodexOAuthTokenPattern.ReplaceAllStringFunc(input, replacer)
	output = openAITokenLikePattern.ReplaceAllStringFunc(output, replacer)
	return output
}
