package currency

import (
	"arbitrage/utils"
	"fmt"
	"strings"
)

type Currency struct {
	Symbol string
}

func (c Currency) String() string {
	return strings.ToUpper(c.Symbol)
}

type Pair struct {
	Base  Currency // Base asset (THISxxx)
	Quote Currency // Quote asset (xxxTHAT)
}

func (p Pair) String() string {
	return strings.ToUpper(p.Base.Symbol + p.Quote.Symbol)
}

var (
	USDC = Currency{Symbol: "USDC"}
	BTC  = Currency{Symbol: "BTC"}
	ETH  = Currency{Symbol: "ETH"}
	BNB  = Currency{Symbol: "BNB"}

	BaseCurrencies = [4]Currency{USDC, BTC, ETH, BNB}

	ADA    = Currency{Symbol: "ADA"}
	AI     = Currency{Symbol: "AI"}
	ALT    = Currency{Symbol: "ALT"}
	ARKM   = Currency{Symbol: "ARKM"}
	AVAX   = Currency{Symbol: "AVAX"}
	AXS    = Currency{Symbol: "AXS"}
	BABY   = Currency{Symbol: "BABY"}
	BANANA = Currency{Symbol: "BANANA"}
	BB     = Currency{Symbol: "BB"}
	BERA   = Currency{Symbol: "BERA"}
	BCH    = Currency{Symbol: "BCH"}
	BIO    = Currency{Symbol: "BIO"}
	BMT    = Currency{Symbol: "BMT"}
	CAKE   = Currency{Symbol: "CAKE"}
	CTK    = Currency{Symbol: "CTK"}
	CYBER  = Currency{Symbol: "CYBER"}
	DOT    = Currency{Symbol: "DOT"}
	EGLD   = Currency{Symbol: "EGLD"}
	ENA    = Currency{Symbol: "ENA"}
	ERA    = Currency{Symbol: "ERA"}
	ETC    = Currency{Symbol: "ETC"}
	FET    = Currency{Symbol: "FET"}
	GPS    = Currency{Symbol: "GPS"}
	GUN    = Currency{Symbol: "GUN"}
	HBAR   = Currency{Symbol: "HBAR"}
	HOME   = Currency{Symbol: "HOME"}
	HUMA   = Currency{Symbol: "HUMA"}
	HYPER  = Currency{Symbol: "HYPER"}
	CHZ    = Currency{Symbol: "CHZ"}
	INIT   = Currency{Symbol: "INIT"}
	INJ    = Currency{Symbol: "INJ"}
	IO     = Currency{Symbol: "IO"}
	KERNEL = Currency{Symbol: "KERNEL"}
	LA     = Currency{Symbol: "LA"}
	LAYER  = Currency{Symbol: "LAYER"}
	LINK   = Currency{Symbol: "LINK"}
	LISTA  = Currency{Symbol: "LISTA"}
	LPT    = Currency{Symbol: "LPT"}
	LTC    = Currency{Symbol: "LTC"}
	MOVE   = Currency{Symbol: "MOVE"}
	NEAR   = Currency{Symbol: "NEAR"}
	NEWT   = Currency{Symbol: "NEWT"}
	NIL    = Currency{Symbol: "NIL"}
	NTRN   = Currency{Symbol: "NTRN"}
	NXPC   = Currency{Symbol: "NXPC"}
	PARTI  = Currency{Symbol: "PARTI"}
	POL    = Currency{Symbol: "POL"}
	PORTAL = Currency{Symbol: "PORTAL"}
	RESOLV = Currency{Symbol: "RESOLV"}
	SAGA   = Currency{Symbol: "SAGA"}
	SAHARA = Currency{Symbol: "SAHARA"}
	S      = Currency{Symbol: "S"}
	SEI    = Currency{Symbol: "SEI"}
	SHELL  = Currency{Symbol: "SHELL"}
	SIGN   = Currency{Symbol: "SIGN"}
	SOL    = Currency{Symbol: "SOL"}
	SOLV   = Currency{Symbol: "SOLV"}
	SOPH   = Currency{Symbol: "SOPH"}
	SPK    = Currency{Symbol: "SPK"}
	STO    = Currency{Symbol: "STO"}
	STX    = Currency{Symbol: "STX"}
	SUI    = Currency{Symbol: "SUI"}
	SXT    = Currency{Symbol: "SXT"}
	TRX    = Currency{Symbol: "TRX"}
	VET    = Currency{Symbol: "VET"}
	WCT    = Currency{Symbol: "WCT"}
	XRP    = Currency{Symbol: "XRP"}
	XVS    = Currency{Symbol: "XVS"}

	Currencies = []Currency{ADA, AI, ALT, ARKM, AVAX, AXS, BABY, BANANA, BB, BERA, BCH, BIO, BMT,
		CAKE, CTK, CYBER, DOT, EGLD, ENA, ERA, ETC, FET, GPS, GUN, HBAR, HOME,
		HUMA, HYPER, CHZ, INIT, INJ, IO, KERNEL, LA, LAYER, LINK, LISTA, LPT, LTC, MOVE,
		NEAR, NEWT, NIL, NTRN, NXPC, PARTI, POL, PORTAL, RESOLV, SAGA, SAHARA, S,
		SEI, SHELL, SIGN, SOL, SOLV, SOPH, SPK, STO, STX, SUI, SXT, TRX, VET, WCT, XRP, XVS}

	DOT_USDC = Pair{Base: DOT, Quote: USDC}
	BNB_USDC = Pair{Base: BNB, Quote: USDC}
	BTC_USDC = Pair{Base: BTC, Quote: USDC}
	ETH_USDC = Pair{Base: ETH, Quote: USDC}
	LTC_USDC = Pair{Base: LTC, Quote: USDC}

	ADA_BTC = Pair{Base: ADA, Quote: BTC}
	LTC_BTC = Pair{Base: LTC, Quote: BTC}
	BNB_BTC = Pair{Base: BNB, Quote: BTC}
	DOT_BTC = Pair{Base: DOT, Quote: BTC}
	ETH_BTC = Pair{Base: ETH, Quote: BTC}
	XRP_BTC = Pair{Base: XRP, Quote: BTC}
	TRX_BTC = Pair{Base: TRX, Quote: BTC}

	ADA_BNB = Pair{Base: ADA, Quote: BNB}
	LTC_BNB = Pair{Base: LTC, Quote: BNB}
	DOT_BNB = Pair{Base: DOT, Quote: BNB}
	XRP_BNB = Pair{Base: XRP, Quote: BNB}
	TRX_BNB = Pair{Base: TRX, Quote: BNB}

	ADA_ETH = Pair{Base: ADA, Quote: ETH}
	BNB_ETH = Pair{Base: BNB, Quote: ETH}
	DOT_ETH = Pair{Base: DOT, Quote: ETH}
	XRP_ETH = Pair{Base: XRP, Quote: ETH}
	LTC_ETH = Pair{Base: LTC, Quote: ETH}
)

var AllSymbols map[string]Pair = make(map[string]Pair)
var BTCSymbols map[string]*Pair = make(map[string]*Pair)
var USDCSymbols map[string]*Pair = make(map[string]*Pair)
var BNBSymbols map[string]*Pair = make(map[string]*Pair)
var ETHSymbols map[string]*Pair = make(map[string]*Pair)

func FillPairs() {
	if len(AllSymbols) > 0 {
		return
	}

	for _, base := range BaseCurrencies {
		pairs, err := utils.ReadLines(utils.GetRoot() + "/pairs_" + strings.ToLower(base.Symbol) + ".txt")
		if err != nil {
			fmt.Println("Error reading", base.Symbol, "pairs:", err)
			continue
		}

		for _, symbol := range pairs {
			from, found := strings.CutSuffix(symbol, base.Symbol)
			if !found {
				fmt.Println("Invalid pair", symbol)
				continue
			}

			pair := Pair{Base: Currency{Symbol: from}, Quote: base}
			AllSymbols[symbol] = pair

			switch base.Symbol {
			case "BTC":
				BTCSymbols[symbol] = &pair
			case "USDC":
				USDCSymbols[symbol] = &pair
			case "BNB":
				BNBSymbols[symbol] = &pair
			case "ETH":
				ETHSymbols[symbol] = &pair
			}
		}
	}
}
