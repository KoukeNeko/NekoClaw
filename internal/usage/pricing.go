package usage

import "strings"

type ModelPricing struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

var pricingTable = map[string]ModelPricing{
	"gemini-2.5-pro":       {InputPerMillion: 1.25, OutputPerMillion: 10.0},
	"gemini-2.5-flash":     {InputPerMillion: 0.15, OutputPerMillion: 0.6},
	"gemini-3-pro-preview": {InputPerMillion: 1.25, OutputPerMillion: 10.0},
	"gemini-3-pro":         {InputPerMillion: 1.25, OutputPerMillion: 10.0},
	"gemini-3-flash":       {InputPerMillion: 0.15, OutputPerMillion: 0.6},
	"gemini-2.0-flash":     {InputPerMillion: 0.10, OutputPerMillion: 0.4},
	"claude-opus-4":        {InputPerMillion: 15.0, OutputPerMillion: 75.0},
	"claude-sonnet-4":      {InputPerMillion: 3.0, OutputPerMillion: 15.0},
	"claude-haiku-3":       {InputPerMillion: 0.25, OutputPerMillion: 1.25},
	"gpt-5":                {InputPerMillion: 2.0, OutputPerMillion: 8.0},
	"gpt-5.1":              {InputPerMillion: 2.0, OutputPerMillion: 8.0},
	"gpt-5.3":              {InputPerMillion: 2.0, OutputPerMillion: 8.0},
	"gpt-4.1":              {InputPerMillion: 2.0, OutputPerMillion: 8.0},
	"gpt-4o":               {InputPerMillion: 2.5, OutputPerMillion: 10.0},
	"o3":                   {InputPerMillion: 2.0, OutputPerMillion: 8.0},
	"o4-mini":              {InputPerMillion: 1.1, OutputPerMillion: 4.4},
}

func LookupPricing(model string) (ModelPricing, bool) {
	model = strings.TrimSpace(model)
	if model == "" || strings.EqualFold(model, "default") {
		return ModelPricing{}, false
	}
	if pricing, ok := pricingTable[model]; ok {
		return pricing, true
	}
	bestLen := 0
	var best ModelPricing
	var found bool
	for prefix, pricing := range pricingTable {
		if len(prefix) > bestLen && strings.HasPrefix(model, prefix) {
			bestLen = len(prefix)
			best = pricing
			found = true
		}
	}
	return best, found
}

func CalculateCost(inputTokens, outputTokens int, model string) float64 {
	pricing, found := LookupPricing(model)
	if !found {
		return 0
	}
	inputCost := float64(inputTokens) * pricing.InputPerMillion / 1_000_000
	outputCost := float64(outputTokens) * pricing.OutputPerMillion / 1_000_000
	return inputCost + outputCost
}
