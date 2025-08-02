package currency

import (
	"fmt"
	"testing"
)

func BenchmarkFillPairs(b *testing.B) {
	FillPairs()
	for _, symbol := range AllSymbols {
		fmt.Println(symbol)
	}
	fmt.Println("AllPairs: ", len(AllSymbols))
}
