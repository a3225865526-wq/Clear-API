package billingexpr

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Frozen design fixtures from the H1 acceptance evidence: two resellers that
// reported ~4.4K prompt tokens (3840 cached) for a tiny visible request.
const (
	fixtureAExpr = `len <= 272000 ? tier("upstream_1", p*0.02 + cr*0.002 + cc*0.025 + cc1h*0.025 + c*0.10) : tier("upstream_2", p*0.04 + cr*0.04 + cc*0.05 + cc1h*0.05 + c*0.15)`
	fixtureBExpr = `len <= 272000 ? tier("upstream_1", p*0.24 + cr*0.012 + cc*0.30 + cc1h*0.30 + c*1.20) : tier("upstream_2", p*0.48 + cr*0.024 + cc*0.60 + cc1h*0.60 + c*1.80)`
)

func TestReservationDomainUpperBoundFixtureCold(t *testing.T) {
	// Cold profile: the whole hidden allowance may land as ordinary input.
	coldA := UsageDomain{P: 4496, CR: 0, CC: 0, CC1h: 0, Len: 4496, C: 30}
	coldB := UsageDomain{P: 4496, CR: 0, CC: 0, CC1h: 0, Len: 4496, C: 30}
	upperA, err := ReservationDomainUpperBound(fixtureAExpr, coldA)
	require.NoError(t, err)
	// (4496*0.02 + 30*0.10) / 1e6 * 500000 * 1.10 = 51.106 -> ceil 52
	assert.InDelta(t, 92.92, upperA, 1e-6)
	assert.Equal(t, 52.0, math.Ceil(upperA/1_000_000*500_000*1.10))

	upperB, err := ReservationDomainUpperBound(fixtureBExpr, coldB)
	require.NoError(t, err)
	// (4496*0.24 + 30*1.20) / 1e6 * 500000 * 1.10 = 613.272 -> ceil 614
	assert.InDelta(t, 1115.04, upperB, 1e-6)
	assert.Equal(t, 614.0, math.Ceil(upperB/1_000_000*500_000*1.10))
}

func TestReservationDomainUpperBoundFixtureWarm(t *testing.T) {
	// Warm typed profile: hidden ordinary <= 656-16, cache read <= 3840.
	warmA := UsageDomain{P: 656, CR: 3840, CC: 0, CC1h: 0, Len: 4496, C: 30}
	upperA, err := ReservationDomainUpperBound(fixtureAExpr, warmA)
	require.NoError(t, err)
	// (656*0.02 + 3840*0.002 + 30*0.10) * 0.5 * 1.10 = 13.09 -> ceil 14
	assert.InDelta(t, 23.8, upperA, 1e-6)
	assert.Equal(t, 14.0, math.Ceil(upperA/1_000_000*500_000*1.10))

	warmB := UsageDomain{P: 656, CR: 3840, CC: 0, CC1h: 0, Len: 4496, C: 30}
	upperB, err := ReservationDomainUpperBound(fixtureBExpr, warmB)
	require.NoError(t, err)
	// (656*0.24 + 3840*0.012 + 30*1.20) * 0.5 * 1.10 = 131.736 -> ceil 132
	assert.InDelta(t, 239.52, upperB, 1e-6)
	assert.Equal(t, 132.0, math.Ceil(upperB/1_000_000*500_000*1.10))
}

func TestReservationUpperBoundRebuildsHistoricalAllBranchReserve(t *testing.T) {
	// The frozen candidate reserves with the all-branch upper bound: tier2
	// dominates although actual usage matched tier1. V=16, O=30 rebuild.
	upperA, err := ReservationUpperBound(fixtureAExpr, 16, 30, 1)
	require.NoError(t, err)
	assert.InDelta(t, 7.38, upperA, 1e-9) // round(7.38/1e6*500000) = round(3.69) = 4
	assert.Equal(t, 4, QuotaRound(upperA/1_000_000*500_000))

	upperB, err := ReservationUpperBound(fixtureBExpr, 16, 30, 1)
	require.NoError(t, err)
	assert.InDelta(t, 81.264, upperB, 1e-9) // round(40.632) = 41
	assert.Equal(t, 41, QuotaRound(upperB/1_000_000*500_000))
}

func TestReservationDomainUpperBoundPrunesProvenBranch(t *testing.T) {
	// Len max 4496 cannot cross the 272000 boundary: tier2 must be pruned.
	domain := UsageDomain{P: 4496, CR: 0, CC: 0, CC1h: 0, Len: 4496, C: 30}
	upper, err := ReservationDomainUpperBound(fixtureAExpr, domain)
	require.NoError(t, err)
	tier2Only, err := ReservationDomainUpperBound(`tier("upstream_2", p*0.04 + cr*0.04 + cc*0.05 + cc1h*0.05 + c*0.15)`, domain)
	require.NoError(t, err)
	assert.Less(t, upper, tier2Only, "proven-unreachable tier2 must not dominate the bound")
	assert.InDelta(t, 92.92, upper, 1e-6)

	// Crossing the boundary (tier crossing test): 270000 visible + 4000 hidden.
	cross := UsageDomain{P: 274000, Len: 274000, C: 30}
	upperB, err := ReservationDomainUpperBound(fixtureBExpr, cross)
	require.NoError(t, err)
	// (274000*0.48 + 30*1.80) * 0.5 * 1.10 = 72365.7 -> 72366
	assert.InDelta(t, 131574.0, upperB, 1e-6)
	assert.Equal(t, 72366.0, math.Ceil(upperB/1_000_000*500_000*1.10))
}

func TestReservationDomainUpperBoundKeepsUndecidableBranches(t *testing.T) {
	// Len can be below or above the boundary: both branches stay candidates.
	domain := UsageDomain{P: 300000, Len: 300000, C: 30}
	upper, err := ReservationDomainUpperBound(fixtureAExpr, domain)
	require.NoError(t, err)
	tier2, err := ReservationDomainUpperBound(`tier("upstream_2", p*0.04 + cr*0.04 + cc*0.05 + cc1h*0.05 + c*0.15)`, UsageDomain{P: 300000, Len: 300000, C: 30})
	require.NoError(t, err)
	assert.Equal(t, tier2, upper, "undecidable tier condition must keep the expensive branch")

	// Request-dependent conditions can never be decided from the domain.
	upper = 0
	upper, err = ReservationDomainUpperBound(`(header("fast")=="yes" ? 6 : 1) * tier("base", p)`, UsageDomain{P: 1000, Len: 1000})
	require.NoError(t, err)
	assert.InDelta(t, 6000.0, upper, 1e-6)
}

func TestReservationDomainUpperBoundZeroBoundAndInvalid(t *testing.T) {
	// A zero cache-read allowance proves cr contributes nothing even when the
	// expression prices it.
	upper, err := ReservationDomainUpperBound(`tier("base", p*2 + cr*50 + c*3)`, UsageDomain{P: 100, CR: 0, Len: 100, C: 10})
	require.NoError(t, err)
	assert.InDelta(t, 230.0, upper, 1e-6)

	_, err = ReservationDomainUpperBound(`tier("base", p*param("rate"))`, UsageDomain{P: 10, Len: 10})
	require.Error(t, err)
	_, err = ReservationDomainUpperBound(`tier("base", 100/c)`, UsageDomain{P: 10, C: 0, Len: 10})
	require.Error(t, err)
	_, err = ReservationDomainUpperBound(`tier("base", p)`, UsageDomain{P: -1, Len: 0})
	require.Error(t, err)
	_, err = ReservationDomainUpperBound(`tier("base", p)`, UsageDomain{P: math.Inf(1), Len: 0})
	require.Error(t, err)
}

// TestReservationDomainUpperBoundSoundAgainstSampledUsage is the core
// soundness property: for random usage realizations inside the declared
// domain, the evaluated expression cost never exceeds the domain bound.
func TestReservationDomainUpperBoundSoundAgainstSampledUsage(t *testing.T) {
	expressions := []string{
		fixtureAExpr,
		fixtureBExpr,
		`tier("base", p*3 + c*15 + cc*6 + cr*0.5 + cc1h*2 + img*4 + img_cr*1 + ai*7 + img_o*9 + ao*8)`,
		`len < 500 ? tier("cheap", p) : tier("expensive", p*30 + c*150)`,
		`len <= 1000 ? (len <= 500 ? tier("a", p+c) : tier("b", p*2+c*3)) : tier("c", p*10+c*20)`,
		`tier("base", max(p, c) * 4 + min(cr, cc) * 3)`,
		`tier("base", c*(100-c) + abs(p-50))`,
	}
	rng := rand.New(rand.NewSource(20261007))
	for _, expression := range expressions {
		for range 400 {
			domain := UsageDomain{
				P:     float64(rng.Intn(5000)),
				CR:    float64(rng.Intn(4000)),
				CC:    float64(rng.Intn(1000)),
				CC1h:  float64(rng.Intn(1000)),
				Img:   float64(rng.Intn(500)),
				ImgCR: float64(rng.Intn(500)),
				AI:    float64(rng.Intn(300)),
				C:     float64(rng.Intn(200)),
				ImgO:  float64(rng.Intn(200)),
				AO:    float64(rng.Intn(200)),
				Len:   float64(rng.Intn(280000)),
			}
			bound, err := ReservationDomainUpperBound(expression, domain)
			if err != nil {
				continue // unsupported expression shapes are rejected, not bounded
			}
			// Sample several realizations strictly inside every per-variable maximum.
			for range 6 {
				params := TokenParams{
					P:     domain.P * rng.Float64(),
					CR:    domain.CR * rng.Float64(),
					CC:    domain.CC * rng.Float64(),
					CC1h:  domain.CC1h * rng.Float64(),
					Img:   domain.Img * rng.Float64(),
					ImgCR: domain.ImgCR * rng.Float64(),
					AI:    domain.AI * rng.Float64(),
					C:     domain.C * rng.Float64(),
					ImgO:  domain.ImgO * rng.Float64(),
					AO:    domain.AO * rng.Float64(),
					Len:   domain.Len * rng.Float64(),
				}
				cost, _, err := RunExpr(expression, params)
				require.NoError(t, err)
				assert.LessOrEqual(t, cost, bound+1e-9,
					"expression %q cost %v exceeds domain bound %v for params %+v", expression, cost, bound, params)
			}
		}
	}
}
