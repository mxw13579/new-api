/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, it } from 'bun:test'

import { categorizeModels, getModelCategory } from './model-categories'

describe('model category classification', () => {
  it('classifies representative model IDs from supported vendors', () => {
    const cases = [
      ['gpt-4.1', 'OpenAI'],
      ['anthropic/claude-3-7-sonnet', 'Anthropic'],
      ['google/gemini-2.5-pro', 'Gemini'],
      ['xai/grok-4', 'xAI'],
      ['deepseek-r1', 'DeepSeek'],
      ['qwen-max', 'Qwen'],
      ['moonshot/kimi-k2', 'Moonshot'],
      ['zai-org/glm-4.5', 'Zhipu'],
      ['mistral-large-latest', 'Mistral'],
      ['meta-llama/llama-3.3-70b-instruct', 'Meta'],
      ['amazon.nova-pro-v1:0', 'Amazon'],
      ['black-forest-labs/flux.1-dev', 'Black Forest Labs'],
    ] as const

    for (const [model, category] of cases) {
      expect(getModelCategory(model)).toBe(category)
    }
  })

  it('prioritizes Perplexity and NVIDIA platform IDs over Meta family names', () => {
    expect(getModelCategory('perplexity/sonar-reasoning-meta-llama-3.1')).toBe(
      'Perplexity'
    )
    expect(getModelCategory('nvidia/llama-3.1-nemotron-70b-instruct')).toBe(
      'NVIDIA'
    )
  })

  it('recognizes bounded OpenAI o1, o3, and o4 model IDs', () => {
    expect(getModelCategory('o1')).toBe('OpenAI')
    expect(getModelCategory('openrouter/o3-mini')).toBe('OpenAI')
    expect(getModelCategory('azure.o4:preview')).toBe('OpenAI')
  })

  it('does not classify similar but unbounded or unsupported o-series names', () => {
    expect(getModelCategory('vendor-o1')).toBe('Other')
    expect(getModelCategory('o2-mini')).toBe('Other')
    expect(getModelCategory('o4mini')).toBe('Other')
    expect(getModelCategory('o10-preview')).toBe('Other')
  })

  it('normalizes surrounding whitespace and letter case before matching', () => {
    expect(getModelCategory('  AnThRoPiC/ClAuDe-3-7-SoNnEt  ')).toBe(
      'Anthropic'
    )
  })

  it('falls back to Other for unknown and empty model names', () => {
    expect(getModelCategory('custom-provider/model-v1')).toBe('Other')
    expect(getModelCategory('   ')).toBe('Other')
  })
})

describe('model category grouping', () => {
  it('preserves first-seen category order and input order within each group', () => {
    const models = [
      'custom-provider/model-v1',
      'gpt-4.1',
      'claude-3-7-sonnet',
      'gpt-4o-mini',
      'custom-provider/model-v2',
      'sonar-pro',
    ] as const

    const categories = categorizeModels(models)

    expect(Object.keys(categories)).toEqual([
      'Other',
      'OpenAI',
      'Anthropic',
      'Perplexity',
    ])
    expect(categories).toEqual({
      Other: ['custom-provider/model-v1', 'custom-provider/model-v2'],
      OpenAI: ['gpt-4.1', 'gpt-4o-mini'],
      Anthropic: ['claude-3-7-sonnet'],
      Perplexity: ['sonar-pro'],
    })
  })
})
