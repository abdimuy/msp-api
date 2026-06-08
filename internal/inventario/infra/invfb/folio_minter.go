//nolint:misspell // Microsip column names are Spanish by convention.
package invfb

import (
	"context"
	"fmt"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
	"github.com/abdimuy/msp-api/internal/inventario/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// folioDigits is the number of numeric digits in the folio body.
const folioDigits = 6

// folioPorPrefijo is the number of folios per prefix block (MST…MSU etc.).
const folioPorPrefijo = 999_999

// folioBaseOffsets represents "MST" as 0-indexed offsets from 'A'.
// M = 12, S = 18, T = 19.
var folioBaseOffsets = [3]int{12, 18, 19}

// numeroAFolio maps an integer sequence number to a folio string such as
// "MST000001". The algorithm cycles through prefixes MST → MSU → … → MSZ →
// MTA → … → ZZZ, each prefix block covering 999 999 folios.
//
// Invariants (tested in folio_mapper_test.go):
//
//	numeroAFolio(1)       == "MST000001"
//	numeroAFolio(999999)  == "MST999999"
//	numeroAFolio(1000000) == "MSU000001"
//	numeroAFolio(1999998) == "MSU999999"
//	numeroAFolio(1999999) == "MSV000001"
func numeroAFolio(numero int) string {
	prefixBlocks := (numero - 1) / folioPorPrefijo
	rest := ((numero - 1) % folioPorPrefijo) + 1

	base := folioBaseOffsets
	carry := prefixBlocks
	for i := 2; i >= 0; i-- {
		total := base[i] + carry
		base[i] = total % 26
		carry = total / 26
	}

	prefix := string([]byte{
		byte('A' + base[0]),
		byte('A' + base[1]),
		byte('A' + base[2]),
	})
	return fmt.Sprintf("%s%0*d", prefix, folioDigits, rest)
}

// FolioMinterFB implements [outbound.FolioMinter] against Microsip's
// GEN_MST_FOLIO sequence.
type FolioMinterFB struct {
	pool *firebird.Pool
}

// NewFolioMinter returns a FolioMinterFB wired to the given pool.
func NewFolioMinter(pool *firebird.Pool) *FolioMinterFB {
	return &FolioMinterFB{pool: pool}
}

// Compile-time check.
var _ outbound.FolioMinter = (*FolioMinterFB)(nil)

// MintFolio atomically claims the next value from GEN_MST_FOLIO and maps it to
// a domain.Folio using the MST/MSU/… cycling algorithm.
func (fm *FolioMinterFB) MintFolio(ctx context.Context) (domain.Folio, error) {
	q := firebird.GetQuerier(ctx, fm.pool.DB)

	var nextVal int
	if err := q.QueryRowContext(ctx, selectNextFolio).Scan(&nextVal); err != nil {
		return domain.Folio{}, fmt.Errorf("invfb MintFolio: GEN_ID(GEN_MST_FOLIO): %w", firebird.MapError(err))
	}

	folioStr := numeroAFolio(nextVal)
	folio, err := domain.NewFolio(folioStr)
	if err != nil {
		// The only way this can fail is if numeroAFolio produced an invalid
		// string, which is a programming error.
		return domain.Folio{}, fmt.Errorf("invfb MintFolio: invalid folio %q from seq=%d: %w",
			folioStr, nextVal, err)
	}
	return folio, nil
}
