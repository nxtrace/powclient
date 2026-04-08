package powclient

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"
)

func TestSolveSemiprime(t *testing.T) {
	largeP1 := big.NewInt(24801309629)
	largeP2 := big.NewInt(34244502967)
	largeSemiprime := new(big.Int).Mul(largeP1, largeP2).String()

	testCases := []struct {
		name      string
		challenge string
		want      []string
		wantErr   error
	}{
		{
			name:      "distinct primes",
			challenge: "35",
			want:      []string{"5", "7"},
		},
		{
			name:      "repeated prime",
			challenge: "25",
			want:      []string{"5", "5"},
		},
		{
			name:      "large semiprime",
			challenge: largeSemiprime,
			want:      []string{largeP1.String(), largeP2.String()},
		},
		{
			name:      "one is invalid",
			challenge: "1",
			wantErr:   ErrInvalidChallenge,
		},
		{
			name:      "zero is invalid",
			challenge: "0",
			wantErr:   ErrInvalidChallenge,
		},
		{
			name:      "negative is invalid",
			challenge: "-35",
			wantErr:   ErrInvalidChallenge,
		},
		{
			name:      "prime is invalid",
			challenge: "13",
			wantErr:   ErrInvalidChallenge,
		},
		{
			name:      "more than two prime factors is invalid",
			challenge: "30",
			wantErr:   ErrInvalidChallenge,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			factors, err := solveSemiprime(context.Background(), tc.challenge)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("solveSemiprime(%q) error = %v, want %v", tc.challenge, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("solveSemiprime(%q) error = %v", tc.challenge, err)
			}
			if len(factors) != len(tc.want) {
				t.Fatalf("solveSemiprime(%q) len = %d, want %d", tc.challenge, len(factors), len(tc.want))
			}
			for i, want := range tc.want {
				if factors[i].String() != want {
					t.Fatalf("solveSemiprime(%q)[%d] = %s, want %s", tc.challenge, i, factors[i].String(), want)
				}
			}
		})
	}
}

func TestSolveSemiprimeHonorsContextDeadline(t *testing.T) {
	originalHook := pollardRhoStepHook
	pollardRhoStepHook = func() {
		time.Sleep(2 * time.Millisecond)
	}
	defer func() {
		pollardRhoStepHook = originalHook
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	_, err := solveSemiprime(ctx, "849387260465695603243")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("solveSemiprime deadline error = %v, want %v", err, context.DeadlineExceeded)
	}
}
