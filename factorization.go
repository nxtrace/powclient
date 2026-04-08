package powclient

import (
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"math/big"
	"sort"
)

const primeCheckRounds = 32

var (
	bigZero = big.NewInt(0)
	bigOne  = big.NewInt(1)
	bigTwo  = big.NewInt(2)

	smallPrimeFactors = []*big.Int{
		big.NewInt(2), big.NewInt(3), big.NewInt(5), big.NewInt(7), big.NewInt(11),
		big.NewInt(13), big.NewInt(17), big.NewInt(19), big.NewInt(23), big.NewInt(29),
		big.NewInt(31), big.NewInt(37), big.NewInt(41), big.NewInt(43), big.NewInt(47),
		big.NewInt(53), big.NewInt(59), big.NewInt(61), big.NewInt(67), big.NewInt(71),
		big.NewInt(73), big.NewInt(79), big.NewInt(83), big.NewInt(89), big.NewInt(97),
	}

	pollardRhoStepHook func()
)

func solveSemiprime(ctx context.Context, challenge string) ([]*big.Int, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	n, ok := new(big.Int).SetString(challenge, 10)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrInvalidChallenge, challenge)
	}
	if n.Cmp(bigTwo) < 0 {
		return nil, fmt.Errorf("%w: challenge must be > 1: %q", ErrInvalidChallenge, challenge)
	}

	factors, err := factorIntoPrimes(ctx, n)
	if err != nil {
		return nil, err
	}
	if len(factors) != 2 {
		return nil, fmt.Errorf("%w: expected exactly two prime factors, got %d", ErrInvalidChallenge, len(factors))
	}

	sort.Slice(factors, func(i, j int) bool {
		return factors[i].Cmp(factors[j]) < 0
	})

	return factors, nil
}

func factorIntoPrimes(ctx context.Context, n *big.Int) ([]*big.Int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if n == nil || n.Sign() <= 0 {
		return nil, fmt.Errorf("%w: challenge must be a positive integer", ErrInvalidChallenge)
	}
	if n.Cmp(bigOne) == 0 {
		return []*big.Int{}, nil
	}
	if n.ProbablyPrime(primeCheckRounds) {
		return []*big.Int{new(big.Int).Set(n)}, nil
	}

	factor, err := findNonTrivialFactor(ctx, n)
	if err != nil {
		return nil, err
	}
	if factor == nil || factor.Cmp(bigOne) <= 0 || factor.Cmp(n) >= 0 {
		return nil, fmt.Errorf("%w: failed to find a non-trivial factor", ErrInvalidChallenge)
	}

	other := new(big.Int).Div(new(big.Int).Set(n), factor)
	left, err := factorIntoPrimes(ctx, factor)
	if err != nil {
		return nil, err
	}
	right, err := factorIntoPrimes(ctx, other)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

func findNonTrivialFactor(ctx context.Context, n *big.Int) (*big.Int, error) {
	if factor, err := findSmallFactor(ctx, n); err != nil || factor != nil {
		return factor, err
	}
	return pollardRhoFactor(ctx, n)
}

func findSmallFactor(ctx context.Context, n *big.Int) (*big.Int, error) {
	remainder := new(big.Int)
	for _, candidate := range smallPrimeFactors {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remainder.Mod(n, candidate)
		if remainder.Cmp(bigZero) == 0 {
			return new(big.Int).Set(candidate), nil
		}
	}
	return nil, nil
}

func pollardRhoFactor(ctx context.Context, n *big.Int) (*big.Int, error) {
	if n.Bit(0) == 0 {
		return new(big.Int).Set(bigTwo), nil
	}

	maxX := new(big.Int).Sub(n, bigTwo)
	maxC := new(big.Int).Sub(n, bigOne)
	diff := new(big.Int)
	divisor := new(big.Int)

	for attempt := 0; attempt < 24; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		x, err := randomBetween(bigTwo, maxX)
		if err != nil {
			return nil, err
		}
		y := new(big.Int).Set(x)
		c, err := randomBetween(bigOne, maxC)
		if err != nil {
			return nil, err
		}
		divisor.Set(bigOne)

		for iter := 0; iter < 1<<16; iter++ {
			if pollardRhoStepHook != nil {
				pollardRhoStepHook()
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			rhoStep(x, c, n)
			rhoStep(y, c, n)
			rhoStep(y, c, n)

			diff.Sub(x, y)
			diff.Abs(diff)
			divisor.GCD(nil, nil, diff, n)

			switch divisor.Cmp(bigOne) {
			case 0:
				continue
			default:
				if divisor.Cmp(n) != 0 {
					return new(big.Int).Set(divisor), nil
				}
			}
			break
		}
	}

	return nil, fmt.Errorf("%w: factorization failed", ErrInvalidChallenge)
}

func rhoStep(x, c, n *big.Int) {
	x.Mul(x, x)
	x.Add(x, c)
	x.Mod(x, n)
}

func randomBetween(minInclusive, maxInclusive *big.Int) (*big.Int, error) {
	if minInclusive == nil || maxInclusive == nil || minInclusive.Cmp(maxInclusive) > 0 {
		return nil, fmt.Errorf("invalid random range")
	}

	span := new(big.Int).Sub(maxInclusive, minInclusive)
	span.Add(span, bigOne)

	value, err := cryptorand.Int(cryptorand.Reader, span)
	if err != nil {
		return nil, err
	}
	value.Add(value, minInclusive)
	return value, nil
}
