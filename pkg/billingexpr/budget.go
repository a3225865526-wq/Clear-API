package billingexpr

import (
	"fmt"
	"math"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

type costInterval struct{ low, high float64 }

// ReservationUpperBound bounds every pricing branch, including non-monotonic
// expressions. Evaluating one sample cannot establish a safe reservation.
// Unsupported arithmetic fails before a paid upstream request is submitted.
func ReservationUpperBound(expression string, input, output float64, imageCount int) (float64, error) {
	if input < 0 || output < 0 || math.IsInf(input, 0) || math.IsInf(output, 0) || math.IsNaN(input) || math.IsNaN(output) {
		return 0, fmt.Errorf("invalid billing budget")
	}
	if _, err := CompileFromCache(expression); err != nil {
		return 0, err
	}
	_, body := ParseExprVersion(expression)
	tree, err := parser.Parse(body)
	if err != nil {
		return 0, err
	}
	bounds, err := boundCost(tree.Node, input, output, float64(max(1, imageCount)))
	if err != nil {
		return 0, err
	}
	if bounds.high < 0 || math.IsInf(bounds.high, 0) || math.IsNaN(bounds.high) {
		return 0, fmt.Errorf("billing expression has no finite non-negative budget")
	}
	return bounds.high, nil
}

func boundCost(node ast.Node, input, output, images float64) (costInterval, error) {
	var result costInterval
	switch n := node.(type) {
	case *ast.IntegerNode:
		result = costInterval{float64(n.Value), float64(n.Value)}
	case *ast.FloatNode:
		result = costInterval{n.Value, n.Value}
	case *ast.IdentifierNode:
		switch n.Value {
		case "p", "len", "cr", "cc", "cc1h", "img", "img_cr", "ai":
			result.high = input
		case "c", "img_o", "ao":
			result.high = output
		case "image_count":
			result = costInterval{images, images}
		default:
			return result, fmt.Errorf("unbounded billing variable %q", n.Value)
		}
	case *ast.ConditionalNode:
		a, err := boundCost(n.Exp1, input, output, images)
		if err != nil {
			return result, err
		}
		b, err := boundCost(n.Exp2, input, output, images)
		if err != nil {
			return result, err
		}
		result = costInterval{min(a.low, b.low), max(a.high, b.high)}
	case *ast.UnaryNode:
		a, err := boundCost(n.Node, input, output, images)
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
		a, err := boundCost(n.Left, input, output, images)
		if err != nil {
			return result, err
		}
		b, err := boundCost(n.Right, input, output, images)
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
			return boundCost(args[1], input, output, images)
		}
		if len(args) < 1 || len(args) > 2 {
			return result, fmt.Errorf("unsupported billing budget arguments")
		}
		a, err := boundCost(args[0], input, output, images)
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
			b, err := boundCost(args[1], input, output, images)
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
