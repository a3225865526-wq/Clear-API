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
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowLeft, ArrowDown, ArrowUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

import {
  getRouteChainAttempts,
  getRouteChainDecisions,
  getRouteChainDetail,
} from './api'
import { AttemptsPanel, DecisionsPanel } from './components/activity-panels'
import { CandidateDiagnostics } from './components/candidate-diagnostics'
import { GateBanner } from './components/gate-banner'
import { RuntimePanel } from './components/runtime-panel'
import {
  useCandidateToggle,
  useChainReorder,
  useCircuitForceOpen,
  useCircuitReset,
} from './hooks/use-chain-mutations'
import {
  routeChainAttemptsKey,
  routeChainDecisionsKey,
  routeChainDetailKey,
} from './lib/query-keys'
import type { ChainDetail } from './types'

// Detail page (§二十, §四十七): clearly separated Configuration / Runtime
// Health / Recent Activity sections, low-frequency polling, server-confirmed
// mutations only.

export function RouteChainDetailPage(props: { chainId: number }) {
  const { chainId } = props
  const { t } = useTranslation()

  const detailQuery = useDetailQuery(chainId)
  const chain = detailQuery.data

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>
        <span className='flex min-w-0 items-center gap-2'>
          <Link
            to='/route-chains'
            aria-label={t('Route Chains')}
            className={buttonVariants({ variant: 'ghost', size: 'icon-xs' })}
          >
            <ArrowLeft />
          </Link>
          <span className='truncate'>
            {chain ? `${chain.model} / ${chain.group}` : t('Route Chain')}
          </span>
          {chain ? (
            <Badge variant='outline'>
              {t('Version')} v{chain.version}
            </Badge>
          ) : null}
        </span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <DetailBody query={detailQuery} chain={chain} />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function DetailBody(props: {
  query: ReturnType<typeof useDetailQuery>
  chain: ChainDetail | undefined
}) {
  const { t } = useTranslation()
  if (props.query.isPending) {
    return <LoadingState message={t('Loading route chain…')} />
  }
  if (props.query.isError) {
    return (
      <ErrorState
        title={t('Failed to load route chain')}
        onRetry={() => void props.query.refetch()}
      />
    )
  }
  if (props.chain) {
    return <DetailContent chain={props.chain} />
  }
  return null
}

function useDetailQuery(chainId: number) {
  return useQuery({
    queryKey: routeChainDetailKey(chainId),
    queryFn: ({ signal }) => getRouteChainDetail(chainId, signal),
    refetchInterval: 10_000,
    refetchIntervalInBackground: false,
    retry: false,
  })
}

function DetailContent(props: { chain: ChainDetail }) {
  const { chain } = props
  const { t } = useTranslation()
  const toggleCandidate = useCandidateToggle()
  const reorder = useChainReorder()
  const resetCircuit = useCircuitReset()
  const forceOpen = useCircuitForceOpen()

  const attemptsQuery = useQuery({
    queryKey: routeChainAttemptsKey(chain.id),
    queryFn: ({ signal }) => getRouteChainAttempts(chain.id, { limit: 20 }, signal),
    refetchInterval: 15_000,
    refetchIntervalInBackground: false,
  })
  const decisionsQuery = useQuery({
    queryKey: routeChainDecisionsKey(chain.id),
    queryFn: ({ signal }) => getRouteChainDecisions(chain.id, { limit: 20 }, signal),
    refetchInterval: 15_000,
    refetchIntervalInBackground: false,
  })

  const orderedActive = [...chain.candidates].sort(
    (a, b) => b.priority - a.priority
  )

  const moveCandidate = (candidateId: number, direction: -1 | 1) => {
    const index = orderedActive.findIndex((candidate) => candidate.id === candidateId)
    const target = index + direction
    if (index < 0 || target < 0 || target >= orderedActive.length) return
    const next = [...orderedActive]
    ;[next[index], next[target]] = [next[target], next[index]]
    reorder.mutate({
      id: chain.id,
      orderedCandidateIds: next.map((candidate) => candidate.id),
      expectedVersion: chain.version,
    })
  }

  const circuitActionPending =
    resetCircuit.isPending || forceOpen.isPending || reorder.isPending

  return (
    <div className='flex flex-col gap-4'>
      <GateBanner gates={chain.gates} />
      {chain.archived ? (
        <div className='rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm'>
          {t(
            'This route chain is archived: read-only, always disabled, kept so attempt history stays explainable.'
          )}
        </div>
      ) : null}

      <Card data-testid='section-configuration'>
        <CardHeader className='pb-2'>
          <CardTitle className='text-base'>
            {t('Configuration')}
            <Badge variant={chain.enabled ? 'default' : 'outline'} className='ml-2'>
              {chain.enabled ? t('Chain Enabled') : t('Chain Disabled')}
            </Badge>
            <Badge variant='outline' className='ml-1'>
              {chain.allow_cross_source
                ? t('Cross-source allowed')
                : t('Cross-source blocked')}
            </Badge>
          </CardTitle>
        </CardHeader>
        <CardContent className='flex flex-col gap-3'>
          {orderedActive.length === 0 ? (
            <EmptyState title={t('No active candidates')} bordered />
          ) : (
            orderedActive.map((candidate, index) => (
              <CandidateDiagnostics
                key={candidate.id}
                candidate={candidate}
                togglePending={toggleCandidate.isPending}
                onToggleEnabled={(enabled) =>
                  toggleCandidate.mutate({
                    chainId: chain.id,
                    candidateId: candidate.id,
                    enabled,
                    expectedVersion: chain.version,
                  })
                }
                actions={
                  <div className='flex items-center gap-1'>
                    <Button
                      variant='ghost'
                      size='icon-xs'
                      aria-label={t('Move candidate up')}
                      disabled={index === 0 || circuitActionPending}
                      onClick={() => moveCandidate(candidate.id, -1)}
                    >
                      <ArrowUp />
                    </Button>
                    <Button
                      variant='ghost'
                      size='icon-xs'
                      aria-label={t('Move candidate down')}
                      disabled={index === orderedActive.length - 1 || circuitActionPending}
                      onClick={() => moveCandidate(candidate.id, 1)}
                    >
                      <ArrowDown />
                    </Button>
                  </div>
                }
              />
            ))
          )}
          {chain.archived_candidates.length > 0 ? (
            <div className='flex flex-col gap-2'>
              <h4 className='text-sm font-medium text-muted-foreground'>
                {t('Archived candidates')}
              </h4>
              {chain.archived_candidates.map((candidate) => (
                <CandidateDiagnostics key={candidate.id} candidate={candidate} />
              ))}
            </div>
          ) : null}
        </CardContent>
      </Card>

      <Card data-testid='section-runtime'>
        <CardHeader className='pb-2'>
          <CardTitle className='text-base'>{t('Runtime Health')}</CardTitle>
        </CardHeader>
        <CardContent>
          <RuntimePanel
            chainId={chain.id}
            candidates={chain.candidates}
            gates={chain.gates}
            actionPending={circuitActionPending}
            onReset={(candidateId) =>
              resetCircuit.mutate({ chainId: chain.id, candidateId })
            }
            onForceOpen={(candidateId, durationSeconds) =>
              forceOpen.mutate({ chainId: chain.id, candidateId, durationSeconds })
            }
          />
        </CardContent>
      </Card>

      <Card data-testid='section-decisions'>
        <CardHeader className='pb-2'>
          <CardTitle className='text-base'>{t('Recent Decisions')}</CardTitle>
        </CardHeader>
        <CardContent>
          {decisionsQuery.data && decisionsQuery.data.length > 0 ? (
            <DecisionsPanel decisions={decisionsQuery.data} />
          ) : (
            <p className='text-sm text-muted-foreground'>{t('No decisions recorded yet.')}</p>
          )}
        </CardContent>
      </Card>

      <Card data-testid='section-attempts'>
        <CardHeader className='pb-2'>
          <CardTitle className='text-base'>{t('Recent Attempts')}</CardTitle>
        </CardHeader>
        <CardContent>
          {attemptsQuery.data && attemptsQuery.data.length > 0 ? (
            <AttemptsPanel attempts={attemptsQuery.data} />
          ) : (
            <p className='text-sm text-muted-foreground'>{t('No attempts recorded yet.')}</p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
