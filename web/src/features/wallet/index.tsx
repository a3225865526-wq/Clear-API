import { ReceiptText } from 'lucide-react'
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
import { useState, useEffect, useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { useIsAdmin } from '@/hooks/use-admin'
import { getSelf } from '@/lib/api'

import { CreditPurchaseCard } from './components/credit-purchase-card'
import { BalanceTransactionsDialog } from './components/dialogs/balance-transactions-dialog'
import { BillingHistoryDialog } from './components/dialogs/billing-history-dialog'
import { OtherRedeemMethodsCard } from './components/other-redeem-methods-card'
import { StoreProductsCard } from './components/store-products-card'
import { StoreRedeemCard } from './components/store-redeem-card'
import { VipProgressCard } from './components/vip-progress-card'
import { WalletStatsCard } from './components/wallet-stats-card'
import { useTopupInfo } from './hooks'
import { disableOnlinePayments } from './lib/disable-online-payments'
import type { UserWalletData } from './types'

interface WalletProps {
  initialShowHistory?: boolean
}

/**
 * 钱包页（当前产品模式：第三方商城兑换码充值）。
 *
 * 在线支付（Epay/Stripe/Creem/Waffo/WaffoPancake）UI 已整体下线：
 * 后端 DisableOnlinePayments=true 时 GetTopUpInfo 不再下发任何支付入口，
 * 前端再经 disableOnlinePayments 兜底清零，本页只渲染
 * 余额 / 商城档位 / 商城兑换 / VIP / 折叠的其他兑换方式。
 * 支付组件（RechargeFormCard、PaymentConfirmDialog、CreemConfirmDialog、
 * SubscriptionPlansCard、AffiliateRewardsCard 等）全部保留在 components/
 * 中，恢复在线支付时重新挂载即可。
 */
export function Wallet(props: WalletProps) {
  const { t } = useTranslation()
  const [user, setUser] = useState<UserWalletData | null>(null)
  const [userLoading, setUserLoading] = useState(true)
  const [billingDialogOpen, setBillingDialogOpen] = useState(false)
  const [balanceDialogOpen, setBalanceDialogOpen] = useState(false)

  const [legacyRedemptionOpen, setLegacyRedemptionOpen] = useState(0)
  const isAdmin = useIsAdmin()
  const {
    topupInfo,
    loading: topupLoading,
    refetch: refetchTopupInfo,
  } = useTopupInfo()
  const checkoutDisabledTopupInfo = useMemo(
    () => disableOnlinePayments(topupInfo),
    [topupInfo]
  )

  // Fetch and refresh user data
  const fetchUser = useCallback(async () => {
    try {
      setUserLoading(true)
      const response = await getSelf()
      if (response.success && response.data) {
        setUser(response.data as UserWalletData)
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to fetch user data:', error)
    } finally {
      setUserLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchUser()
  }, [fetchUser])

  useEffect(() => {
    if (props.initialShowHistory) {
      setBillingDialogOpen(true)
      window.history.replaceState({}, '', window.location.pathname)
    }
  }, [props.initialShowHistory])

  // Refresh user balance + topup info (VIP status, store products) after a
  // store code redemption succeeds. No page reload.
  const handleStoreRedeemSuccess = useCallback(async () => {
    await Promise.all([fetchUser(), refetchTopupInfo()])
  }, [fetchUser, refetchTopupInfo])

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Wallet')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <div className='mx-auto flex w-full max-w-7xl flex-col gap-4 sm:gap-5'>
            <WalletStatsCard
              user={user}
              loading={userLoading}
              vip={checkoutDisabledTopupInfo?.vip ?? null}
              onOpenBalanceDetails={() => setBalanceDialogOpen(true)}
            />

            {topupInfo?.store_recharge_enabled ? (
              <StoreProductsCard
                enabled={topupInfo.store_recharge_enabled}
                products={topupInfo.store_products}
              />
            ) : null}

            {topupInfo?.store_recharge_enabled ? (
              <StoreRedeemCard onSuccess={handleStoreRedeemSuccess} />
            ) : null}

            <VipProgressCard vip={topupInfo?.vip} loading={topupLoading} />

            <CreditPurchaseCard
              onRedeem={() => setLegacyRedemptionOpen((request) => request + 1)}
            />
            <OtherRedeemMethodsCard
              onSuccess={fetchUser}
              openLegacy={legacyRedemptionOpen}
            />

            {isAdmin ? (
              <Button
                variant='ghost'
                size='sm'
                className='text-muted-foreground self-start text-xs'
                onClick={() => setBillingDialogOpen(true)}
              >
                <ReceiptText className='h-3 w-3' />
                {t('Recharge history')}
              </Button>
            ) : null}
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <BillingHistoryDialog
        open={billingDialogOpen}
        onOpenChange={setBillingDialogOpen}
      />

      <BalanceTransactionsDialog
        open={balanceDialogOpen}
        onOpenChange={setBalanceDialogOpen}
      />
    </>
  )
}
