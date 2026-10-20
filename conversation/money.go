package conversation

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/mrmohebi/divar-alert/divar"
)

var moneyMentionPattern = regexp.MustCompile(`[0-9۰-۹٠-٩]+(?:[.,٫٬][0-9۰-۹٠-٩]+)*(?:\s*(?:هزار|میلیون|میلیارد))?\s*(?:تومان|تومن|ریال)`)

// ParseMoney converts explicit Persian monetary amounts to exact integer tomans.
// It never supplies an implied million/billion multiplier for colloquial amounts.
func ParseMoney(text string) (string, error) {
	text = divar.NormalizePersian(text)
	multiplier := int64(1)
	for _, unit := range []struct {
		name   string
		factor int64
	}{{"میلیارد", 1000000000}, {"میلیون", 1000000}, {"هزار", 1000}} {
		if strings.Contains(text, unit.name) {
			multiplier = unit.factor
			text = strings.ReplaceAll(text, unit.name, "")
			break
		}
	}
	rial := strings.Contains(text, "ریال")
	for _, unit := range []string{"تومان", "تومن", "ریال", ",", " "} {
		text = strings.ReplaceAll(text, unit, "")
	}
	number, ok := new(big.Rat).SetString(text)
	if !ok || number.Sign() < 0 {
		return "", fmt.Errorf("invalid monetary amount")
	}
	number.Mul(number, new(big.Rat).SetInt64(multiplier))
	if rial {
		number.Quo(number, new(big.Rat).SetInt64(10))
	}
	if !number.IsInt() || !number.Num().IsInt64() {
		return "", fmt.Errorf("amount must fit integer tomans")
	}
	return number.Num().String(), nil
}

func moneyMentions(messages []string) map[string]string {
	out := map[string]string{}
	for _, message := range messages {
		for _, phrase := range moneyMentionPattern.FindAllString(message, -1) {
			if value, err := ParseMoney(phrase); err == nil {
				out[phrase] = value
			}
		}
	}
	return out
}
