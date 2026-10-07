package billingexpr

import (
	"fmt"
	"math"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// UsageDomain declares an independent upper bound per billing variable for
// reservation estimation. The bound is sound for every actual usage whose each
// variable stays within its declared maximum; the variables may correlate in
// any way because every leaf is bounded separately. Len caps the
// tier-selection input length (visible + every hidden input category).
type UsageDomain struct {
	P          float64
	CR         float64
	CC         float64
	CC1h       float64
	Img        float64
	ImgCR      float64
	AI         float64
	C          float64
	ImgO       float64
	AO         float64
	Len        float64
	ImageCount int
}

// ReservationDomainUpperBound bounds the expression over a typed usage domain.
// Unlike ReservationUpperBound it keeps per-category maxima separate and prunes
// conditional branches whose interval arithmetic proves them unreachable for
// the whole domain. Decidability is never assumed: an undecidable condition
// keeps the maximum of both branches, and unsupported arithmetic still fails
// before a paid upstream request is submitted.
func ReservationDomainUpperBound(expression string, domain UsageDomain) (float64, error) {
	vars, err := domainVars(domain)
	if err != nil {
		return 0, err
	}
	high, err := boundedUpperBound(expression, vars, true)
	if err != nil {
		return 0, err
	}
	return soundHigh(high)
}

func domainVars(domain UsageDomain) (varBounds, error) {
	values := []float64{domain.P, domain.CR, domain.CC, domain.CC1h, domain.Img, domain.ImgCR, domain.AI, domain.C, domain.ImgO, domain.AO, domain.Len}
	for _, value := range values {
		if value < 0 || math.IsInf(value, 0) || math.IsNaN(value) {
			return varBounds{}, fmt.Errorf("invalid billing usage domain")
		}
	}
	return varBounds{
		p: domain.P, cr: domain.CR, cc: domain.CC, cc1h: domain.CC1h,
		img: domain.Img, imgCR: domain.ImgCR, ai: domain.AI,
		c: domain.C, imgO: domain.ImgO, ao: domain.AO,
		len: domain.Len, images: float64(max(1, domain.ImageCount)),
	}, nil
}

// varBounds holds one maximum per billing variable. Zero is a valid bound that
// proves a variable cannot contribute cost.
type varBounds struct {
	p, cr, cc, cc1h, img, imgCR, ai float64
	c, imgO, ao                     float64
	len                             float64
	images                          float64
}

func boundedUpperBound(expression string, vars varBounds, prune bool) (float64, error) {
	if _, err := CompileFromCache(expression); err != nil {
		return 0, err
	}
	_, body := ParseExprVersion(expression)
	tree, err := parser.Parse(body)
	if err != nil {
		return 0, err
	}
	bounds, err := boundCost(tree.Node, vars, prune)
	if err != nil {
		return 0, err
	}
	if bounds.high < 0 || math.IsInf(bounds.high, 0) || math.IsNaN(bounds.high) {
		return 0, fmt.Errorf("billing expression has no finite non-negative budget")
	}
	return bounds.high, nil
}

// soundHigh inflates an interval high so it stays a sound envelope of the
// evaluation VM: integer-only subtrees compute in exact int64 and convert to
// float64 at the end, while interval arithmetic accumulates float64 rounding
// per operation. The relative term dominates deep trees; the absolute term
// covers values near zero. Applied on the hardening authorization path only —
// the frozen legacy bound keeps its exact frozen values.
func soundHigh(high float64) (float64, error) {
	if high < 0 || math.IsInf(high, 0) || math.IsNaN(high) {
		return 0, fmt.Errorf("billing expression has no finite non-negative budget")
	}
	high += math.Abs(high)*1e-12 + 1e-9
	if math.IsInf(high, 0) || math.IsNaN(high) {
		return 0, fmt.Errorf("billing expression has no finite non-negative budget")
	}
	return high, nil
}

func boundCost(node ast.Node, vars varBounds, prune bool) (costInterval, error) {
	var result costInterval
	switch n := node.(type) {
	case *ast.IntegerNode:
		result = costInterval{float64(n.Value), float64(n.Value)}
	case *ast.FloatNode:
		result = costInterval{n.Value, n.Value}
	case *ast.IdentifierNode:
		switch n.Value {
		case "p":
			result = costInterval{0, vars.p}
		case "len":
			result = costInterval{0, vars.len}
		case "cr":
			result = costInterval{0, vars.cr}
		case "cc":
			result = costInterval{0, vars.cc}
		case "cc1h":
			result = costInterval{0, vars.cc1h}
		case "img":
			result = costInterval{0, vars.img}
		case "img_cr":
			result = costInterval{0, vars.imgCR}
		case "ai":
			result = costInterval{0, vars.ai}
		case "c":
			result = costInterval{0, vars.c}
		case "img_o":
			result = costInterval{0, vars.imgO}
		case "ao":
			result = costInterval{0, vars.ao}
		case "image_count":
			result = costInterval{vars.images, vars.images}
		default:
			return result, fmt.Errorf("unbounded billing variable %q", n.Value)
		}
	case *ast.ConditionalNode:
		if prune {
			switch decideCondition(n.Cond, vars) {
			case triTrue:
				return boundCost(n.Exp1, vars, prune)
			case triFalse:
				return boundCost(n.Exp2, vars, prune)
			}
		}
		a, err := boundCost(n.Exp1, vars, prune)
		if err != nil {
			return result, err
		}
		b, err := boundCost(n.Exp2, vars, prune)
		if err != nil {
			return result, err
		}
		result = costInterval{min(a.low, b.low), max(a.high, b.high)}
	case *ast.UnaryNode:
		a, err := boundCost(n.Node, vars, prune)
		if err != nil {
			return result, err
		}
		switch n.Operator {
		case "+":
			result = a
		case "-":
			result = costInterval{-a.high, -a.low}
		default:
			return result, fmt.Errorf("unsupported billing budget operator")
		}
	case *ast.BinaryNode:
		a, err := boundCost(n.Left, vars, prune)
		if err != nil {
			return result, err
		}
		b, err := boundCost(n.Right, vars, prune)
		if err != nil {
			return result, err
		}
		switch n.Operator {
		case "+":
			result = costInterval{a.low + b.low, a.high + b.high}
		case "-":
			result = costInterval{a.low - b.high, a.high - b.low}
		case "*", "/":
			if n.Operator == "/" {
				if b.low <= 0 && b.high >= 0 {
					return result, fmt.Errorf("billing divisor can be zero")
				}
				b = costInterval{1 / b.high, 1 / b.low}
			}
			values := []float64{a.low * b.low, a.low * b.high, a.high * b.low, a.high * b.high}
			result = costInterval{values[0], values[0]}
			for _, value := range values[1:] {
				result.low = min(result.low, value)
				result.high = max(result.high, value)
			}
		default:
			return result, fmt.Errorf("unsupported billing budget operator %q", n.Operator)
		}
	case *ast.CallNode:
		callee, ok := n.Callee.(*ast.IdentifierNode)
		if !ok {
			return result, fmt.Errorf("unsupported billing budget function")
		}
		args := n.Arguments
		if callee.Value == "tier" && len(args) == 2 {
			return boundCost(args[1], vars, prune)
		}
		if len(args) < 1 || len(args) > 2 {
			return result, fmt.Errorf("unsupported billing budget arguments")
		}
		a, err := boundCost(args[0], vars, prune)
		if err != nil {
			return result, err
		}
		switch callee.Value {
		case "fixed":
			result = costInterval{a.low * 1_000_000, a.high * 1_000_000}
		case "ceil":
			result = costInterval{math.Ceil(a.low), math.Ceil(a.high)}
		case "floor":
			result = costInterval{math.Floor(a.low), math.Floor(a.high)}
		case "abs":
			result = costInterval{0, max(math.Abs(a.low), math.Abs(a.high))}
		case "max", "min":
			if len(args) != 2 {
				return result, fmt.Errorf("invalid billing budget function")
			}
			b, err := boundCost(args[1], vars, prune)
			if err != nil {
				return result, err
			}
			if callee.Value == "max" {
				result = costInterval{max(a.low, b.low), max(a.high, b.high)}
			} else {
				result = costInterval{min(a.low, b.low), min(a.high, b.high)}
			}
		default:
			return result, fmt.Errorf("unsupported billing budget function %q", callee.Value)
		}
	default:
		return result, fmt.Errorf("unsupported billing budget expression")
	}
	if math.IsNaN(result.low) || math.IsNaN(result.high) || math.IsInf(result.low, 0) || math.IsInf(result.high, 0) {
		return result, fmt.Errorf("billing budget overflow")
	}
	return result, nil
}

type triBool int

const (
	triFalse triBool = iota
	triUnknown
	triTrue
)

// decideCondition proves a condition unreachable or always-true over the
// domain using interval arithmetic. Any construct it cannot decide keeps both
// branches, matching the frozen all-branch safety.
func decideCondition(node ast.Node, vars varBounds) triBool {
	switch n := node.(type) {
	case *ast.BinaryNode:
		switch n.Operator {
		case "<", "<=", ">", ">=", "==", "!=":
			a, err := boundCost(n.Left, vars, true)
			if err != nil {
				return triUnknown
			}
			b, err := boundCost(n.Right, vars, true)
			if err != nil {
				return triUnknown
			}
			return decideComparison(n.Operator, a, b)
		case "&&", "and":
			left, right := decideCondition(n.Left, vars), decideCondition(n.Right, vars)
			if left == triFalse || right == triFalse {
				return triFalse
			}
			if left == triTrue && right == triTrue {
				return triTrue
			}
			return triUnknown
		case "||", "or":
			left, right := decideCondition(n.Left, vars), decideCondition(n.Right, vars)
			if left == triTrue || right == triTrue {
				return triTrue
			}
			if left == triFalse && right == triFalse {
				return triFalse
			}
			return triUnknown
		}
	case *ast.UnaryNode:
		if n.Operator == "!" {
			switch decideCondition(n.Node, vars) {
			case triTrue:
				return triFalse
			case triFalse:
				return triTrue
			}
			return triUnknown
		}
	}
	return triUnknown
}

func decideComparison(operator string, a, b costInterval) triBool {
	// Sound rules: the condition is definitely true only when every
	// realization of a and b satisfies it, definitely false only when no
	// realization can, and unknown otherwise.
	switch operator {
	case "<":
		if a.high < b.low {
			return triTrue
		}
		if a.low >= b.high {
			return triFalse
		}
		return triUnknown
	case "<=":
		if a.high <= b.low {
			return triTrue
		}
		if a.low > b.high {
			return triFalse
		}
		return triUnknown
	case ">":
		if a.low > b.high {
			return triTrue
		}
		if a.high <= b.low {
			return triFalse
		}
		return triUnknown
	case ">=":
		if a.low >= b.high {
			return triTrue
		}
		if a.high < b.low {
			return triFalse
		}
		return triUnknown
	case "==":
		if a.low == a.high && b.low == b.high && a.low == b.low {
			return triTrue
		}
		if a.high < b.low || b.high < a.low {
			return triFalse
		}
		return triUnknown
	case "!=":
		if a.low == a.high && b.low == b.high && a.low != b.low {
			return triTrue
		}
		if a.high < b.low || b.high < a.low {
			return triTrue
		}
		if a.low == a.high && b.low == b.high {
			return triFalse
		}
		return triUnknown
	}
	return triUnknown
}
