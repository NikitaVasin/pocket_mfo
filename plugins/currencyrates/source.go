package currencyrates

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
)

const sourceURL = "https://www.cbr.ru/scripts/XML_daily.asp"
const maxResponseSize = 2 << 20

type rate struct {
	CBRID    string  `xml:"ID,attr"`
	Currency string  `xml:"CharCode"`
	NumCode  string  `xml:"NumCode"`
	Name     string  `xml:"Name"`
	Nominal  int64   `xml:"Nominal"`
	RawValue string  `xml:"Value"`
	Value    float64 `xml:"-"`
}

type dailyRates struct {
	XMLName xml.Name  `xml:"ValCurs"`
	RawDate string    `xml:"Date,attr"`
	Rates   []rate    `xml:"Valute"`
	Date    time.Time `xml:"-"`
}

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
var numCodePattern = regexp.MustCompile(`^[0-9]{3}$`)
var valuePattern = regexp.MustCompile(`^[0-9]+,[0-9]+$`)

func fetch(ctx context.Context, client *http.Client, date time.Time) (dailyRates, error) {
	u := sourceURL + "?" + url.Values{"date_req": {date.Format("02/01/2006")}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return dailyRates{}, err
	}
	// Identify the integration: CBR rejects Go's default User-Agent with HTTP 403.
	req.Header.Set("User-Agent", "PocketMFO-CurrencyRates (+https://github.com/NikitaVasin/pocket_mfo)")
	resp, err := client.Do(req)
	if err != nil {
		return dailyRates{}, fmt.Errorf("currencyrates: fetch CBR: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return dailyRates{}, fmt.Errorf("currencyrates: CBR returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return dailyRates{}, fmt.Errorf("currencyrates: read CBR: %w", err)
	}
	if len(body) > maxResponseSize {
		return dailyRates{}, fmt.Errorf("currencyrates: CBR response too large")
	}
	return parse(body)
}

func parse(body []byte) (dailyRates, error) {
	var result dailyRates
	decoder := xml.NewDecoder(bytes.NewReader(body))
	decoder.CharsetReader = charset.NewReaderLabel // CBR publishes Windows-1251 XML.
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("currencyrates: invalid CBR XML: %w", err)
	}
	// Reject a second document or malformed trailing data before persisting anything.
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, fmt.Errorf("currencyrates: invalid trailing XML: %w", err)
		}
		switch v := token.(type) {
		case xml.CharData:
			if strings.TrimSpace(string(v)) == "" {
				continue
			}
		case xml.Comment:
			continue
		}
		return result, fmt.Errorf("currencyrates: unexpected trailing XML")
	}
	date, err := time.Parse("02.01.2006", result.RawDate)
	if err != nil || len(result.Rates) == 0 {
		return result, fmt.Errorf("currencyrates: missing rates or invalid effective date")
	}
	result.Date = date
	seen := make(map[string]bool, len(result.Rates))
	for i := range result.Rates {
		r := &result.Rates[i]
		r.Name = strings.TrimSpace(r.Name)
		if !currencyPattern.MatchString(r.Currency) || !numCodePattern.MatchString(r.NumCode) || r.CBRID == "" || r.Name == "" || r.Nominal <= 0 || seen[r.Currency] {
			return result, fmt.Errorf("currencyrates: invalid or duplicate currency %q", r.Currency)
		}
		seen[r.Currency] = true
		raw := strings.TrimSpace(r.RawValue)
		if !valuePattern.MatchString(raw) {
			return result, fmt.Errorf("currencyrates: invalid value for %s", r.Currency)
		}
		r.Value, err = strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
		if err != nil || math.IsInf(r.Value, 0) || math.IsNaN(r.Value) || r.Value <= 0 || r.Value/float64(r.Nominal) <= 0 {
			return result, fmt.Errorf("currencyrates: invalid value for %s", r.Currency)
		}
	}
	return result, nil
}
