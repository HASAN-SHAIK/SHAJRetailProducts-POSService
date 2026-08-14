package orders

import (
	"errors"
	"strings"
)

var ErrInvalidTaxPolicy = errors.New("invalid tax policy")

type TaxPolicy struct {
	Enabled      bool
	Mode         string
	RoundingMode string
}

func calculateTax(policy TaxPolicy, taxableMinor int64, gstRateBps *int64) (taxMinor int64, lineTotalMinor int64, err error) {
	if taxableMinor < 0 {
		return 0, 0, ErrInvalidOrder
	}
	mode := strings.ToUpper(strings.TrimSpace(policy.Mode))
	rounding := strings.ToUpper(strings.TrimSpace(policy.RoundingMode))
	if mode != "INCLUSIVE" && mode != "EXCLUSIVE" {
		return 0, 0, ErrInvalidTaxPolicy
	}
	if rounding != "HALF_UP" {
		return 0, 0, ErrInvalidTaxPolicy
	}
	if !policy.Enabled || gstRateBps == nil || *gstRateBps == 0 {
		return 0, taxableMinor, nil
	}
	rate := *gstRateBps
	if rate < 0 || rate > 10000 {
		return 0, 0, ErrInvalidTaxPolicy
	}

	denominator := int64(10000)
	if mode == "INCLUSIVE" {
		denominator += rate
	}
	taxMinor = roundHalfUp(taxableMinor*rate, denominator)
	if mode == "INCLUSIVE" {
		return taxMinor, taxableMinor, nil
	}
	return taxMinor, taxableMinor + taxMinor, nil
}

func roundHalfUp(numerator, denominator int64) int64 {
	if numerator <= 0 {
		return 0
	}
	return (numerator + denominator/2) / denominator
}
