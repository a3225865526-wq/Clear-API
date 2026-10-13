import * as z from 'zod'

export const monitorSchema = z
  .object({
    display_name: z.string().trim().min(1).max(255),
    interval_seconds: z.coerce.number().int().min(60).max(86400),
    timeout_seconds: z.coerce.number().int().min(1).max(120),
    stale_seconds: z.coerce.number().int().min(60).max(604800),
    daily_requests: z.coerce.number().int().min(0).max(1440),
    request_cost_micros: z.coerce.number().int().min(0).max(1000000000),
  })
  .refine((value) => value.stale_seconds >= value.interval_seconds, {
    path: ['stale_seconds'],
    message: 'Invalid value',
  })

export type MonitorInput = z.input<typeof monitorSchema>
export type MonitorValues = z.output<typeof monitorSchema>

export interface Monitor extends MonitorValues {
  id: number
  channel_id: number
  target_kind: string
  target_ref: string
  protocol: string
  model: string
  platform: string
  published: boolean
  enabled: boolean
  archived: boolean
  created_by?: number
  created_at?: number
  target_type?: string
  max_concurrency?: number
  daily_budget_limit?: number
}
export interface MonitorTargets {
  channels: { id: number; name: string; status: number; models: string }[]
  groups: { group: string; model: string }[]
}

export const numericFields = [
  ['interval_seconds', 'Probe interval (seconds)'],
  ['timeout_seconds', 'Probe timeout (seconds)'],
  ['stale_seconds', 'Result validity (seconds)'],
  ['daily_requests', 'Daily probe request limit'],
  ['request_cost_micros', 'Reserved cost per probe (micro USD)'],
] as const
