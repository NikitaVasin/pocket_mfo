package partnerlinks

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// conversionData contains only validated scalar attributes. Empty optional fields
// are omitted from analytics and leave previously stored values unchanged.
type conversionData struct {
	Status    string            `json:"status"`
	RawStatus string            `json:"rawStatus"`
	LeadID    string            `json:"leadId,omitempty"`
	EventID   string            `json:"eventId,omitempty"`
	Amount    string            `json:"amount,omitempty"`
	Currency  string            `json:"currency,omitempty"`
	Extra     map[string]string `json:"extra,omitempty"`
}

func parseConversion(values map[string]any, prov *Provider, now int64) (conversionData, int64, error) {
	rawStatus, err := scalar(values, prov.Fields.Status)
	if err != nil {
		return conversionData{}, 0, fmt.Errorf("Некорректный статус")
	}
	status := prov.Statuses[rawStatus]
	if status == "" {
		return conversionData{}, 0, fmt.Errorf("Неизвестный статус")
	}
	conversion := conversionData{Status: status, RawStatus: rawStatus}
	for _, field := range []struct {
		path   string
		target *string
	}{
		{prov.Fields.LeadID, &conversion.LeadID},
		{prov.Fields.EventID, &conversion.EventID},
		{prov.Fields.Amount, &conversion.Amount},
		{prov.Fields.Currency, &conversion.Currency},
	} {
		value, err := scalar(values, field.path)
		if err != nil {
			return conversionData{}, 0, fmt.Errorf("Некорректное поле конверсии")
		}
		*field.target = value
	}
	if conversion.Amount != "" {
		n, err := strconv.ParseFloat(conversion.Amount, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return conversionData{}, 0, fmt.Errorf("Некорректная сумма")
		}
	}
	extra := map[string]string{}
	for key, path := range prov.ExtraFields {
		v, err := scalar(values, path)
		if err != nil {
			return conversionData{}, 0, fmt.Errorf("Некорректный дополнительный параметр")
		}
		if v != "" {
			extra[key] = v
		}
	}
	if len(extra) > 0 {
		conversion.Extra = extra
	}
	timestamp := now
	rawTime, err := scalar(values, prov.Fields.Timestamp)
	if err != nil {
		return conversionData{}, 0, fmt.Errorf("Некорректное время события")
	}
	if rawTime != "" {
		timestamp, err = strconv.ParseInt(rawTime, 10, 64)
		if err != nil {
			return conversionData{}, 0, fmt.Errorf("Время события должно быть Unix timestamp в секундах")
		}
	}
	if timestamp < now-14*86400 || timestamp > now {
		return conversionData{}, 0, fmt.Errorf("Время события должно быть в пределах последних 14 дней")
	}
	return conversion, timestamp, nil
}

func scalar(values map[string]any, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	var value any = values
	// Form/query names are literal; nested paths apply only when no literal key exists.
	if direct, ok := values[path]; ok {
		value = direct
	} else {
		for _, part := range strings.Split(path, ".") {
			m, ok := value.(map[string]any)
			if !ok {
				return "", nil
			}
			value = m[part]
		}
	}
	var result string
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		result = v
	case json.Number:
		result = v.String()
	case bool:
		result = strconv.FormatBool(v)
	default:
		return "", fmt.Errorf("expected scalar postback field")
	}
	// Apply the same bound after conversion: JSON numbers are attacker-controlled too.
	if len(result) > 4096 {
		return "", fmt.Errorf("field too long")
	}
	return result, nil
}

var revenueAmount = regexp.MustCompile(`^(0|[1-9][0-9]{0,9})(\.[0-9]{1,8})?$`)
var revenueCurrency = regexp.MustCompile(`^[A-Z]{3}$`)

func validateRevenue(conversion conversionData) error {
	if !revenueAmount.MatchString(conversion.Amount) || !revenueCurrency.MatchString(conversion.Currency) || strings.TrimSpace(conversion.LeadID) == "" {
		return fmt.Errorf("Для Revenue нужны ID заявки, сумма дохода decimal(10,8) и код валюты из трёх заглавных букв")
	}
	return nil
}
