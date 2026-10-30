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
import { ENDPOINT_TYPES } from '@/features/pricing/constants'

export type GuideClient = 'codex' | 'claude' | 'opencode' | 'cherry'

export const guideClients: Record<
  GuideClient,
  { name: string; endpoint: string }
> = {
  codex: { name: 'Codex CLI', endpoint: ENDPOINT_TYPES.OPENAI_RESPONSE },
  claude: { name: 'Claude Code', endpoint: ENDPOINT_TYPES.ANTHROPIC },
  opencode: { name: 'OpenCode', endpoint: ENDPOINT_TYPES.OPENAI },
  cherry: { name: 'Cherry Studio', endpoint: ENDPOINT_TYPES.OPENAI },
}

export function apiRoot(address: string): string {
  const url = new URL(address)
  if (
    !['https:', 'http:'].includes(url.protocol) ||
    url.username ||
    url.password ||
    url.search ||
    url.hash
  ) {
    throw new Error('Invalid API address')
  }
  return url.href.replace(/\/+$/, '').replace(/\/v1$/, '')
}

export function buildClientConfig(
  client: GuideClient,
  root: string,
  model: string
): string {
  const quotedModel = JSON.stringify(model)
  if (client === 'codex') {
    return `model = ${quotedModel}
model_provider = "clearapi"
web_search = "disabled"

[model_providers.clearapi]
name = "ClearAPI"
base_url = ${JSON.stringify(`${root}/v1`)}
wire_api = "responses"
env_key = "CLEARAPI_API_KEY"
requires_openai_auth = false`
  }
  if (client === 'claude') {
    return JSON.stringify(
      {
        env: {
          ANTHROPIC_BASE_URL: root,
          ANTHROPIC_AUTH_TOKEN: '<YOUR_API_KEY>',
          ANTHROPIC_MODEL: model,
        },
      },
      null,
      2
    )
  }
  if (client === 'opencode') {
    return JSON.stringify(
      {
        $schema: 'https://opencode.ai/config.json',
        model: `clearapi/${model}`,
        provider: {
          clearapi: {
            npm: '@ai-sdk/openai-compatible',
            name: 'ClearAPI',
            options: {
              baseURL: `${root}/v1`,
              apiKey: '{env:CLEARAPI_API_KEY}',
            },
            models: { [model]: { name: model } },
          },
        },
      },
      null,
      2
    )
  }
  return `OpenAI-compatible (Chat Completions)\nBase URL: ${root}/v1\nAPI Key: <YOUR_API_KEY>\nModel: ${model}`
}
