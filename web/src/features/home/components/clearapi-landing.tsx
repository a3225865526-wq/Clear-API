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
import { ArrowDown, ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { Footer } from '@/components/layout/components/footer'
import type { TopNavLink } from '@/components/layout/types'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

import {
  CapabilitiesSection,
  QuickstartSection,
} from './clearapi-landing-sections'

type ClearApiLandingProps = {
  isAuthenticated: boolean
}

function ClearApiMark() {
  return (
    <span
      aria-hidden='true'
      className='relative flex size-7 shrink-0 items-center justify-center overflow-hidden rounded-[10px] bg-gradient-to-br from-teal-500 to-cyan-700 shadow-sm shadow-teal-900/20'
    >
      <span className='text-xs font-semibold text-white'>CA</span>
    </span>
  )
}

function FlowNode(props: { label: string; detail: string }) {
  return (
    <div className='flex min-h-16 min-w-0 flex-1 flex-col justify-center rounded-xl border border-teal-100 bg-white/80 px-4 py-3 shadow-sm dark:border-teal-900 dark:bg-slate-950/70'>
      <span className='truncate text-sm font-semibold text-slate-900 dark:text-slate-100'>
        {props.label}
      </span>
      <span className='mt-1 text-xs leading-relaxed text-slate-500 dark:text-slate-400'>
        {props.detail}
      </span>
    </div>
  )
}

export function ClearApiLanding(props: ClearApiLandingProps) {
  const { t } = useTranslation()
  const navLinks: TopNavLink[] = [
    { title: 'clearapi.landing.nav.models', href: '/pricing' },
    { title: 'clearapi.landing.nav.quickstart', href: '/#quickstart' },
    {
      title: 'clearapi.landing.nav.dashboard',
      href: props.isAuthenticated ? '/dashboard' : '/sign-in',
    },
  ]

  return (
    <PublicLayout
      showMainContainer={false}
      siteName='澄光 API / ClearAPI'
      logo={<ClearApiMark />}
      navLinks={navLinks}
    >
      <main
        aria-labelledby='clearapi-hero-title'
        className='via-background to-background overflow-hidden bg-gradient-to-b from-teal-50/80 dark:from-teal-950/30'
      >
        <section className='relative mx-auto grid max-w-7xl gap-10 px-4 pt-28 pb-16 sm:px-6 md:pt-32 md:pb-24 lg:grid-cols-[1.05fr_0.95fr] lg:items-center lg:gap-14 lg:px-8'>
          <div className='relative z-10'>
            <p className='mb-5 inline-flex items-center gap-2 rounded-full border border-teal-200/80 bg-white/80 px-3 py-1.5 text-xs font-medium tracking-wide text-teal-800 shadow-sm dark:border-teal-900 dark:bg-slate-950/70 dark:text-teal-200'>
              <span className='size-1.5 rounded-full bg-teal-500' />
              {t('clearapi.landing.hero.eyebrow')}
            </p>
            <h1
              id='clearapi-hero-title'
              className='max-w-2xl text-4xl leading-[1.12] font-semibold tracking-tight text-slate-950 sm:text-5xl md:text-6xl dark:text-white'
            >
              {t('clearapi.landing.hero.title')}
            </h1>
            <p className='text-muted-foreground mt-6 max-w-xl text-base leading-7 sm:text-lg sm:leading-8'>
              {t('clearapi.landing.hero.description')}
            </p>
            <div className='mt-8 flex flex-col gap-3 sm:flex-row sm:flex-wrap'>
              <Button
                size='lg'
                className='h-11 rounded-xl bg-teal-700 px-5 text-white hover:bg-teal-800'
                render={
                  <Link
                    to={props.isAuthenticated ? '/dashboard' : '/sign-up'}
                  />
                }
              >
                {t(
                  props.isAuthenticated
                    ? 'clearapi.landing.hero.dashboard'
                    : 'clearapi.landing.hero.register'
                )}
                <ArrowRight aria-hidden='true' />
              </Button>
              <Button
                variant='outline'
                size='lg'
                className='h-11 rounded-xl border-teal-200 bg-white/70 px-5 dark:border-teal-900 dark:bg-slate-950/50'
                render={<Link to='/pricing' />}
              >
                {t('clearapi.landing.hero.pricing')}
              </Button>
            </div>
            <a
              href='#quickstart'
              className='mt-4 inline-flex items-center gap-2 text-sm font-medium text-teal-800 underline-offset-4 hover:underline dark:text-teal-200'
            >
              {t('clearapi.landing.hero.quickstart')}
              <ArrowDown aria-hidden='true' className='size-4' />
            </a>
            <p className='text-muted-foreground/80 mt-4 max-w-lg text-xs leading-5'>
              {t('clearapi.landing.hero.disclaimer')}
            </p>
          </div>

          <Card className='relative border-teal-100/90 bg-white/80 p-1 shadow-[0_24px_80px_-36px_rgba(13,148,136,0.38)] backdrop-blur dark:border-teal-900/70 dark:bg-slate-950/80'>
            <div
              aria-hidden='true'
              className='pointer-events-none absolute -top-12 -right-10 size-40 rounded-full bg-cyan-300/20 blur-3xl'
            />
            <CardHeader className='relative flex-row items-center justify-between gap-4 px-5 pt-5'>
              <CardTitle className='text-sm font-medium text-slate-700 dark:text-slate-200'>
                {t('clearapi.landing.flow.title')}
              </CardTitle>
              <span className='rounded-full bg-teal-50 px-2.5 py-1 text-[11px] font-medium text-teal-800 dark:bg-teal-950 dark:text-teal-200'>
                {t('clearapi.landing.flow.label')}
              </span>
            </CardHeader>
            <CardContent className='relative px-5 pb-5'>
              <div className='flex flex-col items-stretch gap-2 sm:flex-row sm:items-center sm:gap-2'>
                <FlowNode
                  label={t('clearapi.landing.flow.client')}
                  detail={t('clearapi.landing.flow.client_detail')}
                />
                <ArrowRight
                  aria-hidden='true'
                  className='mx-auto hidden size-4 shrink-0 text-teal-600 sm:block'
                />
                <ArrowDown
                  aria-hidden='true'
                  className='mx-auto size-4 shrink-0 text-teal-600 sm:hidden'
                />
                <FlowNode
                  label={t('clearapi.landing.flow.gateway')}
                  detail={t('clearapi.landing.flow.gateway_detail')}
                />
                <ArrowRight
                  aria-hidden='true'
                  className='mx-auto hidden size-4 shrink-0 text-teal-600 sm:block'
                />
                <ArrowDown
                  aria-hidden='true'
                  className='mx-auto size-4 shrink-0 text-teal-600 sm:hidden'
                />
                <FlowNode
                  label={t('clearapi.landing.flow.upstream')}
                  detail={t('clearapi.landing.flow.upstream_detail')}
                />
              </div>
              <p className='text-muted-foreground mt-4 text-xs leading-5'>
                {t('clearapi.landing.flow.note')}
              </p>
            </CardContent>
          </Card>
        </section>

        <CapabilitiesSection />
        <QuickstartSection isAuthenticated={props.isAuthenticated} />
      </main>
      <Footer
        name='澄光 API / ClearAPI'
        description='clearapi.landing.footer.description'
      />
    </PublicLayout>
  )
}
