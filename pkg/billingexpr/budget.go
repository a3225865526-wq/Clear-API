package billingexpr

import (
	"fmt"
	"math"
)

type costInterval struct{ low, high float64 }

// ReservationUpperBound bounds every pricing branch, including non-monotonic
// expressions. Evaluating one sample cannot establish a safe reservation.
// Unsupported arithmetic fails before a paid upstream request is submitted.
//
// Every input variable shares the input maximum and conditional branches are
// never pruned: this is the frozen all-branch safety envelope. The typed
// per-category envelope lives in ReservationDomainUpperBound.
func ReservationUpperBound(expression string, input, output float64, imageCount int) (float64, error) {
	if input < 0 || output < 0 || math.IsInf(input, 0) || math.IsInf(output, 0) || math.IsNaN(input) || math.IsNaN(output) {
		return 0, fmt.Errorf("invalid billing budget")
	}
	vars := varBounds{
		p: input, cr: input, cc: input, cc1h: input, img: input, imgCR: input, ai: input,
		c: output, imgO: output, ao: output,
		len: input, images: float64(max(1, imageCount)),
	}
	return boundedUpperBound(expression, vars, false)
}
