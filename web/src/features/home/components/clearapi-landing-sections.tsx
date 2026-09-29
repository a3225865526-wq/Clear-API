/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or (at your
option) any later version.

This program is distributed in the hope that it will be useful, but WITHOUT
ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
FITNESS FOR A PARTICULAR PURPOSE. See the GNU Affero General Public License
for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { Link } from '@tanstack/react-router'
import { ArrowRight, KeyRound, ListChecks, ScrollText } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

const quickStartCode = [
  'export CLEARAPI_BASE_URL="http://127.0.0.1:3000"',
  'export CLEARAPI_API_KEY="YOUR_API_KEY"',
  '',
  'curl "$CLEARAPI_BASE_URL/v1/chat/completions" \\',
  '  -H "Authorization: Bearer $CLEARAPI_API_KEY" \\',
  '  -H "Content-Type: application/json" \\',
  '  -d \'{"model":"MODEL_NAME_FROM_CATALOG","messages":[{"role":"user","content":"你好"}]}\'',
].join('\n')

function CapabilityCard(props: {
  icon: React.ReactNode
  title: string
  description: string
}) {
  return (
    <Card className='bg-background h-full border-teal-100/80 shadow-sm dark:border-teal-950'>
      <CardHeader>
        <span className='flex size-10 items-center justify-center rounded-xl bg-teal-50 text-teal-800 dark:bg-teal-950 dark:text-teal-200'>
          {props.icon}
        </span>
        <CardTitle className='mt-2 text-base font-semibold'>
          {props.title}
        </CardTitle>
      </CardHeader>
      <CardContent className='text-muted-foreground leading-6'>
        {props.description}
      </CardContent>
    </Card>
  )
}

export function CapabilitiesSection() {
  const { t } = useTranslation()

  return (
    <section
      aria-labelledby='clearapi-capabilities-title'
      className='border-y border-teal-100/70 bg-white/70 py-16 sm:py-20 dark:border-teal-950 dark:bg-slate-950/35'
    >
      <div className='mx-auto max-w-7xl px-4 sm:px-6 lg:px-8'>
        <div className='max-w-2xl'>
          <p className='text-xs font-semibold tracking-[0.16em] text-teal-700 uppercase dark:text-teal-300'>
            {t('clearapi.landing.capabilities.eyebrow')}
          </p>
          <h2
            id='clearapi-capabilities-title'
            className='mt-3 text-2xl font-semibold tracking-tight sm:text-3xl'
          >
            {t('clearapi.landing.capabilities.title')}
          </h2>
        </div>
        <div className='mt-8 grid gap-4 md:grid-cols-3'>
          <CapabilityCard
            icon={<KeyRound aria-hidden='true' />}
            title={t('clearapi.landing.capabilities.keys_title')}
            description={t('clearapi.landing.capabilities.keys_body')}
          />
          <CapabilityCard
            icon={<ListChecks aria-hidden='true' />}
            title={t('clearapi.landing.capabilities.models_title')}
            description={t('clearapi.landing.capabilities.models_body')}
          />
          <CapabilityCard
            icon={<ScrollText aria-hidden='true' />}
            title={t('clearapi.landing.capabilities.usage_title')}
            description={t('clearapi.landing.capabilities.usage_body')}
          />
        </div>
      </div>
    </section>
  )
}

type QuickstartSectionProps = {
  isAuthenticated: boolean
}

export function QuickstartSection(props: QuickstartSectionProps) {
  const { t } = useTranslation()

  return (
    <section
      id='quickstart'
      aria-labelledby='clearapi-quickstart-title'
      className='mx-auto grid max-w-7xl gap-8 px-4 py-16 sm:px-6 sm:py-20 lg:grid-cols-[0.8fr_1.2fr] lg:items-center lg:gap-12 lg:px-8'
    >
      <div>
        <p className='text-xs font-semibold tracking-[0.16em] text-teal-700 uppercase dark:text-teal-300'>
          {t('clearapi.landing.quickstart.eyebrow')}
        </p>
        <h2
          id='clearapi-quickstart-title'
          className='mt-3 text-2xl font-semibold tracking-tight sm:text-3xl'
        >
          {t('clearapi.landing.quickstart.title')}
        </h2>
        <p className='text-muted-foreground mt-4 leading-7'>
          {t('clearapi.landing.quickstart.description')}
        </p>
        <p className='text-muted-foreground mt-3 text-sm leading-6'>
          {t('clearapi.landing.quickstart.note')}
        </p>
        <Link
          to={props.isAuthenticated ? '/dashboard' : '/sign-up'}
          className='mt-5 inline-flex items-center gap-2 text-sm font-medium text-teal-800 underline-offset-4 hover:underline dark:text-teal-200'
        >
          {t('clearapi.landing.quickstart.console_link')}
          <ArrowRight aria-hidden='true' className='size-4' />
        </Link>
      </div>

      <div
        role='region'
        aria-label={t('clearapi.landing.quickstart.code_label')}
        data-code-scroll
        className='min-w-0 overflow-x-auto rounded-2xl border border-slate-800 bg-slate-950 p-4 shadow-xl shadow-slate-950/10 sm:p-6'
        tabIndex={0}
      >
        <pre className='min-w-max text-[12px] leading-6 text-slate-100 sm:text-sm'>
          <code>{quickStartCode}</code>
        </pre>
      </div>
    </section>
  )
}
