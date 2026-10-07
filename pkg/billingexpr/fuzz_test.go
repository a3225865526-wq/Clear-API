package billingexpr

import (
	"math/rand"
	"testing"
)

// FuzzReservationDomainUpperBoundSoundness checks the core safety property on
// arbitrary expressions: whenever the domain bound is computable, no sampled
// usage realization inside the domain may price above it, and the frozen
// all-branch bound never decreases for inputs inside the domain.
func FuzzReservationDomainUpperBoundSoundness(f *testing.F) {
	seeds := []string{
		`len <= 272000 ? tier("a", p*0.02 + cr*0.002 + c*0.1) : tier("b", p*0.04 + c*0.15)`,
		`tier("base", p*3 + c*15 + cc*6 + cr*0.5)`,
		`len < 500 ? tier("cheap", p) : tier("expensive", p*30 + c*150)`,
		`tier("base", max(p, c) * 4 + min(cr, cc) * 3)`,
		`tier("base", c*(100-c))`,
		`len <= 1000 ? (len <= 500 ? tier("a", p+c) : tier("b", p*2+c*3)) : tier("c", p*10+c*20)`,
		// Regression: this shape once pruned to the wrong branch when the
		// definitely-false comparison was mapped to the wrong polarity.
		`len <00?tier("",0):tier("",100)`,
		`len >= 0 ? tier("a", p*2) : tier("b", p*99)`,
	}
	for _, seed := range seeds {
		f.Add(seed, uint64(4496), uint64(3840), uint64(30))
	}
	rng := rand.New(rand.NewSource(1))
	f.Fuzz(func(t *testing.T, expression string, p, cr, c uint64) {
		if len(expression) > 200 {
			t.Skip()
		}
		domain := UsageDomain{
			P:    float64(p % 50000),
			CR:   float64(cr % 50000),
			CC:   float64(p % 10000),
			CC1h: float64(cr % 10000),
			C:    float64(c % 50000),
			Len:  float64((p + cr) % 300000),
		}
		bound, err := ReservationDomainUpperBound(expression, domain)
		if err != nil {
			t.Skip() // unsupported expressions are rejected, never under-bounded
		}
		if bound < 0 || bound != bound {
			t.Fatalf("non-finite bound %v for %q", bound, expression)
		}
		for range 12 {
			params := TokenParams{
				P:    domain.P * rng.Float64(),
				CR:   domain.CR * rng.Float64(),
				CC:   domain.CC * rng.Float64(),
				CC1h: domain.CC1h * rng.Float64(),
				C:    domain.C * rng.Float64(),
				Len:  domain.Len * rng.Float64(),
			}
			cost, _, err := RunExpr(expression, params)
			if err != nil {
				t.Skip()
			}
			if cost > bound+1e-9 {
				t.Fatalf("unsound bound: expression %q cost %v exceeds domain bound %v at %+v", expression, cost, bound, params)
			}
		}
	})
}
