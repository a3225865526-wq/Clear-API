import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import { StaticDataTable } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Form } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { formatQuota } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  createOfflineOrder,
  listOfflineOrders,
  voidOfflineOrder,
  type OfflineOrder,
} from './api'
import {
  offlineOrderSchema,
  toOfflineOrderInput,
  type OfflineOrderValues,
} from './lib/order-form'

function localDateTimeNow() {
  const now = new Date()
  return new Date(now.getTime() - now.getTimezoneOffset() * 60_000)
    .toISOString()
    .slice(0, 16)
}

export function ClearAPIOfflineOrders() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [createdCodes, setCreatedCodes] = useState<string[] | null>(null)
  const [orderToVoid, setOrderToVoid] = useState<OfflineOrder | null>(null)
  const form = useForm<OfflineOrderValues>({
    resolver: zodResolver(offlineOrderSchema),
    defaultValues: {
      orderNo: '',
      moneyYuan: '',
      quotaAmount: 0,
      count: 1,
      paidAt: localDateTimeNow(),
      expiresAt: '',
    },
  })
  const orders = useQuery({
    queryKey: ['clearapi', 'offline-orders', page],
    queryFn: () => listOfflineOrders(page),
  })
  const createOrder = useMutation({
    mutationFn: (values: OfflineOrderValues) =>
      createOfflineOrder(toOfflineOrderInput(values)),
    onSuccess: (codes) => {
      setCreatedCodes(codes)
      form.reset({
        orderNo: '',
        moneyYuan: '',
        quotaAmount: 0,
        count: 1,
        paidAt: localDateTimeNow(),
        expiresAt: '',
      })
      queryClient.invalidateQueries({
        queryKey: ['clearapi', 'offline-orders'],
      })
      toast.success(t('clearapi.offline.created'))
    },
    onError: (error) =>
      handleServerError(error, t('clearapi.offline.createFailed')),
  })
  const voidOrder = useMutation({
    mutationFn: voidOfflineOrder,
    onSuccess: () => {
      setOrderToVoid(null)
      queryClient.invalidateQueries({
        queryKey: ['clearapi', 'offline-orders'],
      })
      toast.success(t('clearapi.offline.voided'))
    },
    onError: (error) =>
      handleServerError(error, t('clearapi.offline.voidFailed')),
  })

  let orderContent: ReactNode
  if (orders.isPending) {
    orderContent = <p>{t('Loading...')}</p>
  } else if (orders.isError) {
    orderContent = (
      <p role='alert' className='text-destructive'>
        {t('clearapi.offline.loadFailed')}
      </p>
    )
  } else if (orders.data.items.length === 0) {
    orderContent = <EmptyState title={t('clearapi.offline.empty')} />
  } else {
    orderContent = (
      <StaticDataTable
        data={orders.data.items}
        getRowKey={(order) => order.id}
        columns={[
          {
            id: 'order',
            header: t('clearapi.offline.orderNo'),
            cell: (order) => order.order_no,
          },
          {
            id: 'money',
            header: t('clearapi.offline.money'),
            cell: (order) => `¥${(order.money_cents / 100).toFixed(2)}`,
          },
          {
            id: 'quota',
            header: t('clearapi.offline.quota'),
            cell: (order) => formatQuota(order.quota_per_code),
          },
          {
            id: 'status',
            header: t('Status'),
            cell: (order) =>
              order.status === 'paid'
                ? t('clearapi.offline.paid')
                : t('clearapi.offline.void'),
          },
          {
            id: 'codes',
            header: t('clearapi.offline.codes'),
            cell: (order) => order.codes.map((code) => code.key).join(', '),
          },
          {
            id: 'actions',
            header: t('Actions'),
            cell: (order) =>
              order.status === 'paid' ? (
                <Button
                  size='sm'
                  variant='outline'
                  onClick={() => setOrderToVoid(order)}
                >
                  {t('clearapi.offline.void')}
                </Button>
              ) : null,
          },
        ]}
      />
    )
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('clearapi.offline.title')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='space-y-6'>
          <p className='text-muted-foreground text-sm'>
            {t('clearapi.offline.description')}
          </p>
          <Card>
            <CardHeader>
              <CardTitle>{t('clearapi.offline.recordSale')}</CardTitle>
            </CardHeader>
            <CardContent>
              <Form {...form}>
                <form
                  className='grid gap-4 sm:grid-cols-2'
                  onSubmit={form.handleSubmit((values) =>
                    createOrder.mutate(values)
                  )}
                >
                  <div className='space-y-2'>
                    <Label htmlFor='offline-order-no'>
                      {t('clearapi.offline.orderNo')}
                    </Label>
                    <Input
                      id='offline-order-no'
                      {...form.register('orderNo')}
                      aria-invalid={!!form.formState.errors.orderNo}
                    />
                    {form.formState.errors.orderNo && (
                      <p className='text-destructive text-xs'>
                        {t('clearapi.offline.orderNoError')}
                      </p>
                    )}
                  </div>
                  <div className='space-y-2'>
                    <Label htmlFor='offline-money'>
                      {t('clearapi.offline.money')}
                    </Label>
                    <Input
                      id='offline-money'
                      inputMode='decimal'
                      {...form.register('moneyYuan')}
                      aria-invalid={!!form.formState.errors.moneyYuan}
                    />
                    {form.formState.errors.moneyYuan && (
                      <p className='text-destructive text-xs'>
                        {t('clearapi.offline.moneyError')}
                      </p>
                    )}
                  </div>
                  <div className='space-y-2'>
                    <Label htmlFor='offline-quota'>
                      {t('clearapi.offline.quota')}
                    </Label>
                    <Input
                      id='offline-quota'
                      type='number'
                      min='0'
                      step='any'
                      {...form.register('quotaAmount', { valueAsNumber: true })}
                      aria-invalid={!!form.formState.errors.quotaAmount}
                    />
                    {form.formState.errors.quotaAmount && (
                      <p className='text-destructive text-xs'>
                        {t('clearapi.offline.quotaError')}
                      </p>
                    )}
                  </div>
                  <div className='space-y-2'>
                    <Label htmlFor='offline-count'>
                      {t('clearapi.offline.count')}
                    </Label>
                    <Input
                      id='offline-count'
                      type='number'
                      min='1'
                      max='100'
                      {...form.register('count', { valueAsNumber: true })}
                      aria-invalid={!!form.formState.errors.count}
                    />
                    {form.formState.errors.count && (
                      <p className='text-destructive text-xs'>
                        {t('clearapi.offline.countError')}
                      </p>
                    )}
                  </div>
                  <div className='space-y-2'>
                    <Label htmlFor='offline-paid-at'>
                      {t('clearapi.offline.paidAt')}
                    </Label>
                    <Input
                      id='offline-paid-at'
                      type='datetime-local'
                      {...form.register('paidAt')}
                      aria-invalid={!!form.formState.errors.paidAt}
                    />
                  </div>
                  <div className='space-y-2'>
                    <Label htmlFor='offline-expires-at'>
                      {t('clearapi.offline.expiresAt')}
                    </Label>
                    <Input
                      id='offline-expires-at'
                      type='datetime-local'
                      {...form.register('expiresAt')}
                    />
                  </div>
                  <div className='sm:col-span-2'>
                    <Button type='submit' disabled={createOrder.isPending}>
                      {t('clearapi.offline.create')}
                    </Button>
                  </div>
                </form>
              </Form>
            </CardContent>
          </Card>
          {createdCodes && (
            <Card>
              <CardHeader>
                <CardTitle>{t('clearapi.offline.codesOnce')}</CardTitle>
              </CardHeader>
              <CardContent className='space-y-2'>
                {createdCodes.map((code) => (
                  <div key={code} className='flex items-center gap-2'>
                    <code className='min-w-0 text-sm break-all'>{code}</code>
                    <CopyButton value={code} />
                  </div>
                ))}
                <Button
                  type='button'
                  variant='outline'
                  onClick={() => setCreatedCodes(null)}
                >
                  {t('clearapi.offline.dismissCodes')}
                </Button>
              </CardContent>
            </Card>
          )}
          <section className='space-y-3'>
            <h2 className='text-lg font-semibold'>
              {t('clearapi.offline.orders')}
            </h2>
            {orderContent}
            <div className='flex items-center justify-end gap-2'>
              <Button
                variant='outline'
                disabled={page <= 1}
                onClick={() => setPage(page - 1)}
              >
                {t('Previous')}
              </Button>
              <span className='text-sm'>{page}</span>
              <Button
                variant='outline'
                disabled={!orders.data || page * 10 >= orders.data.total}
                onClick={() => setPage(page + 1)}
              >
                {t('Next')}
              </Button>
            </div>
          </section>
        </div>
        <ConfirmDialog
          destructive
          open={orderToVoid !== null}
          onOpenChange={(open) => {
            if (!open) setOrderToVoid(null)
          }}
          title={t('clearapi.offline.voidTitle')}
          desc={t('clearapi.offline.voidDescription')}
          confirmText={t('clearapi.offline.void')}
          isLoading={voidOrder.isPending}
          handleConfirm={() => {
            if (orderToVoid) voidOrder.mutate(orderToVoid.id)
          }}
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
