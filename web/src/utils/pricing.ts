/**
 * Model pricing table — mirrors internal/tui/settings_usage.go pricingTable.
 */

interface ModelPricing {
  inputPerMillion: number;
  outputPerMillion: number;
}

const PRICING_TABLE: Record<string, ModelPricing> = {
  "gemini-2.5-pro": { inputPerMillion: 1.25, outputPerMillion: 10.0 },
  "gemini-2.5-flash": { inputPerMillion: 0.15, outputPerMillion: 0.6 },
  "gemini-3-pro-preview": { inputPerMillion: 1.25, outputPerMillion: 10.0 },
  "gemini-3-pro": { inputPerMillion: 1.25, outputPerMillion: 10.0 },
  "gemini-3-flash": { inputPerMillion: 0.15, outputPerMillion: 0.6 },
  "gemini-2.0-flash": { inputPerMillion: 0.1, outputPerMillion: 0.4 },
  "claude-opus-4": { inputPerMillion: 15.0, outputPerMillion: 75.0 },
  "claude-sonnet-4": { inputPerMillion: 3.0, outputPerMillion: 15.0 },
  "claude-haiku-3": { inputPerMillion: 0.25, outputPerMillion: 1.25 },
  "gpt-5": { inputPerMillion: 2.0, outputPerMillion: 8.0 },
  "gpt-5.1": { inputPerMillion: 2.0, outputPerMillion: 8.0 },
  "gpt-5.3": { inputPerMillion: 2.0, outputPerMillion: 8.0 },
  "gpt-4.1": { inputPerMillion: 2.0, outputPerMillion: 8.0 },
  "gpt-4o": { inputPerMillion: 2.5, outputPerMillion: 10.0 },
  "o3": { inputPerMillion: 2.0, outputPerMillion: 8.0 },
  "o4-mini": { inputPerMillion: 1.1, outputPerMillion: 4.4 },
};

/** Longest-prefix lookup, matching Go's lookupModelContextWindow pattern */
function lookupPricing(model: string): ModelPricing | null {
  if (!model) return null;
  // Exact match first
  if (PRICING_TABLE[model]) return PRICING_TABLE[model];
  // Longest prefix
  let bestLen = 0;
  let bestPricing: ModelPricing | null = null;
  for (const [prefix, pricing] of Object.entries(PRICING_TABLE)) {
    if (prefix.length > bestLen && model.startsWith(prefix)) {
      bestLen = prefix.length;
      bestPricing = pricing;
    }
  }
  return bestPricing;
}

/** Calculate cost in USD for a given usage and model */
export function calculateCost(
  inputTokens: number,
  outputTokens: number,
  model: string,
): number {
  const pricing = lookupPricing(model);
  if (!pricing) return 0;
  const inputCost = (inputTokens * pricing.inputPerMillion) / 1_000_000;
  const outputCost = (outputTokens * pricing.outputPerMillion) / 1_000_000;
  return inputCost + outputCost;
}
